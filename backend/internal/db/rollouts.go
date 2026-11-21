package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// A Rollout is what TowerCrane does with one commit of the default branch:
// plan (and apply) every Application in an order that respects
// spec.syncPolicy.dependsOn.
type Rollout struct {
	ID           string         `json:"id"`
	RepositoryID string         `json:"repository_id"`
	CommitSHA    string         `json:"commit_sha"`
	Status       string         `json:"status"` // running, done, superseded
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	Items        []*RolloutItem `json:"items"`
}

// Rollout item statuses.
const (
	ItemWaiting     = "waiting"      // not started (dependencies, order or paused)
	ItemPlanning    = "planning"     // plan running
	ItemAwaitingYes = "awaiting_yes" // plan waits for an approver's "yes"
	ItemApplying    = "applying"     // apply running
	ItemApplied     = "applied"      // done: applied
	ItemNoChanges   = "no_changes"   // done: nothing to apply
	ItemBlocked     = "blocked"      // the policy blocked the apply
	ItemRejected    = "rejected"     // syncPolicy rules rejected the plan
	ItemFailed      = "failed"       // plan or apply failed
	ItemDiscarded   = "discarded"    // someone said "no"
	ItemSkipped     = "skipped"      // a dependency didn't succeed
	ItemSuperseded  = "superseded"   // a newer commit started a new rollout
	ItemUnchanged   = "unchanged"    // not planned: none of its files changed
)

// ItemSucceeded: dependents may go ahead.
func ItemSucceeded(status string) bool {
	return status == ItemApplied || status == ItemNoChanges || status == ItemUnchanged
}

// ItemFinished: the item won't change any more (unless retried).
func ItemFinished(status string) bool {
	switch status {
	case ItemApplied, ItemNoChanges, ItemBlocked, ItemRejected, ItemFailed, ItemDiscarded, ItemSkipped, ItemSuperseded, ItemUnchanged:
		return true
	}
	return false
}

type RolloutItem struct {
	RolloutID   string    `json:"rollout_id"`
	Application string    `json:"application"`
	Position    int       `json:"position"`
	DependsOn   []string  `json:"depends_on"`
	Status      string    `json:"status"`
	Paused      bool      `json:"paused"`
	PlanOpID    string    `json:"plan_op_id,omitempty"`
	ApplyOpID   string    `json:"apply_op_id,omitempty"`
	Message     string    `json:"message,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
	// SourceSHA: commit of the item's code when the rollout started.
	SourceSHA string `json:"source_sha,omitempty"`
}

type RolloutStore struct {
	db *sql.DB
}

func NewRolloutStore(db *sql.DB) *RolloutStore {
	return &RolloutStore{db: db}
}

func (s *RolloutStore) CreateTables() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS rollouts (
		id UUID PRIMARY KEY,
		repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
		commit_sha VARCHAR(40) NOT NULL,
		status VARCHAR(20) NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_rollouts_repo ON rollouts(repository_id, created_at DESC);

	CREATE TABLE IF NOT EXISTS rollout_items (
		rollout_id UUID NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
		application VARCHAR(63) NOT NULL,
		position INTEGER NOT NULL,
		depends_on TEXT NOT NULL,
		status VARCHAR(20) NOT NULL,
		paused BOOLEAN NOT NULL DEFAULT FALSE,
		plan_op_id UUID,
		apply_op_id UUID,
		message TEXT,
		updated_at TIMESTAMP NOT NULL,
		PRIMARY KEY (rollout_id, application)
	);
	CREATE INDEX IF NOT EXISTS idx_rollout_items_plan ON rollout_items(plan_op_id);
	ALTER TABLE rollout_items ALTER COLUMN application TYPE VARCHAR(255);
	-- Commit of the item's code when the rollout started (code may live in
	-- another repository than the rollout's manifests).
	ALTER TABLE rollout_items ADD COLUMN IF NOT EXISTS source_sha VARCHAR(40);
	`)
	return err
}

// FailInterrupted marks rollout items that were planning or applying when
// TowerCrane stopped; an admin can retry them.
func (s *RolloutStore) FailInterrupted() error {
	_, err := s.db.Exec(`UPDATE rollout_items SET status = $1,
		message = 'Interrupted: TowerCrane restarted while this Application was running (retry it)', updated_at = $2
		WHERE status IN ('planning', 'applying')`, ItemFailed, time.Now())
	return err
}

// Create starts a rollout. Any unfinished rollout of the same repository is
// superseded: its items that haven't finished stop where they are.
func (s *RolloutStore) Create(r *Rollout) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	if _, err := tx.Exec(`UPDATE rollout_items SET status = $1, message = 'A newer commit started a new rollout', updated_at = $2
		WHERE rollout_id IN (SELECT id FROM rollouts WHERE repository_id = $3 AND status = 'running')
			AND status IN ('waiting', 'awaiting_yes')`, ItemSuperseded, now, r.RepositoryID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE rollouts SET status = 'superseded', updated_at = $1
		WHERE repository_id = $2 AND status = 'running'`, now, r.RepositoryID); err != nil {
		return err
	}

	r.ID = uuid.New().String()
	r.Status = "running"
	r.CreatedAt, r.UpdatedAt = now, now
	if _, err := tx.Exec(`INSERT INTO rollouts (id, repository_id, commit_sha, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)`, r.ID, r.RepositoryID, r.CommitSHA, r.Status, now); err != nil {
		return err
	}
	for _, it := range r.Items {
		it.RolloutID, it.UpdatedAt = r.ID, now
		if it.Status == "" {
			it.Status = ItemWaiting
		}
		deps, _ := json.Marshal(nonNilStrings(it.DependsOn))
		if _, err := tx.Exec(`INSERT INTO rollout_items (rollout_id, application, position, depends_on, status, message, updated_at, source_sha)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''))`,
			r.ID, it.Application, it.Position, string(deps), it.Status, it.Message, now, it.SourceSHA); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *RolloutStore) items(rolloutID string) ([]*RolloutItem, error) {
	rows, err := s.db.Query(`SELECT rollout_id, application, position, depends_on, status, paused,
		COALESCE(plan_op_id::text, ''), COALESCE(apply_op_id::text, ''), COALESCE(message, ''), updated_at
		FROM rollout_items WHERE rollout_id = $1 ORDER BY position, application`, rolloutID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*RolloutItem{}
	for rows.Next() {
		it := &RolloutItem{}
		var deps string
		if err := rows.Scan(&it.RolloutID, &it.Application, &it.Position, &deps, &it.Status, &it.Paused,
			&it.PlanOpID, &it.ApplyOpID, &it.Message, &it.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(deps), &it.DependsOn)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *RolloutStore) Get(id string) (*Rollout, error) {
	r := &Rollout{}
	if err := s.db.QueryRow(`SELECT id, repository_id, commit_sha, status, created_at, updated_at FROM rollouts WHERE id = $1`, id).
		Scan(&r.ID, &r.RepositoryID, &r.CommitSHA, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	items, err := s.items(id)
	if err != nil {
		return nil, err
	}
	r.Items = items
	return r, nil
}

// ListByRepository returns the most recent rollouts with their items.
func (s *RolloutStore) ListByRepository(repoID string, limit int) ([]*Rollout, error) {
	rows, err := s.db.Query(`SELECT id FROM rollouts WHERE repository_id = $1 ORDER BY created_at DESC LIMIT $2`, repoID, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []*Rollout{}
	for _, id := range ids {
		r, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// UpdateItem stores an item's status, operations and message.
func (s *RolloutStore) UpdateItem(it *RolloutItem) error {
	_, err := s.db.Exec(`UPDATE rollout_items SET status = $1, paused = $2, plan_op_id = NULLIF($3, '')::uuid,
		apply_op_id = NULLIF($4, '')::uuid, message = NULLIF($5, ''), position = $6, updated_at = $7
		WHERE rollout_id = $8 AND application = $9`,
		it.Status, it.Paused, it.PlanOpID, it.ApplyOpID, it.Message, it.Position, time.Now(), it.RolloutID, it.Application)
	return err
}

// FindByPlan returns the rollout item a plan operation belongs to.
func (s *RolloutStore) FindByPlan(planOpID string) (rolloutID, application string, err error) {
	err = s.db.QueryRow(`SELECT rollout_id, application FROM rollout_items WHERE plan_op_id = $1`, planOpID).
		Scan(&rolloutID, &application)
	return
}

// SetStatus updates a rollout's own status.
func (s *RolloutStore) SetStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE rollouts SET status = $1, updated_at = $2 WHERE id = $3`, status, time.Now(), id)
	return err
}

// Settled is where an Application last ended well in a rollout (applied, no
// changes, or unchanged): the manifests' commit and its code's commit.
type Settled struct {
	ConfigSHA string
	SourceSHA string
}

// LastSettled returns where an Application last ended well, or a zero
// Settled if it never did. The code commit is the one its plan really used
// (falling back to the rollout item's, then the manifests' commit).
func (s *RolloutStore) LastSettled(repoID, application string) (Settled, error) {
	var st Settled
	err := s.db.QueryRow(`SELECT r.commit_sha, COALESCE(o.source_commit_sha, i.source_sha, o.commit_sha, r.commit_sha)
		FROM rollout_items i JOIN rollouts r ON r.id = i.rollout_id
		LEFT JOIN operations o ON o.id = i.plan_op_id
		WHERE r.repository_id = $1 AND i.application = $2 AND i.status IN ($3, $4, $5)
		ORDER BY r.created_at DESC LIMIT 1`, repoID, application, ItemApplied, ItemNoChanges, ItemUnchanged).
		Scan(&st.ConfigSHA, &st.SourceSHA)
	if err == sql.ErrNoRows {
		return Settled{}, nil
	}
	return st, err
}
