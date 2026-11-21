package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type Operation struct {
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	UserID       string `json:"user_id"`
	Type         string `json:"type"`                 // plan, apply, destroy
	Status       string `json:"status"`               // pending, running, success, failed
	Environment  string `json:"environment"`          // dev, prod, etc
	Application  string `json:"application"`          // Application name (manifest metadata.name)
	WorkingDir   string `json:"working_dir"`          // path in repo
	CommitSHA    string `json:"commit_sha,omitempty"` // repo commit this operation ran against
	// SourceCommitSHA is the commit of the Application's code: the same as
	// CommitSHA, or the head of its spec.source.repo when the code lives in
	// another repository.
	SourceCommitSHA string `json:"source_commit_sha,omitempty"`
	Plan         string `json:"plan,omitempty"`       // tofu plan output
	Log          string `json:"log"`                  // operation log
	Error        string `json:"error,omitempty"`      // error message if failed

	// Emergency marks an admin "break glass" operation that skipped the
	// TowerCrane policy.
	Emergency bool `json:"emergency"`
	// Plans only: whether the plan has changes, whether it is waiting for an
	// approver's "yes", and what the policy decided about applying it.
	HasChanges           bool   `json:"has_changes"`
	AwaitingConfirmation bool   `json:"awaiting_confirmation"`
	PolicyNote           string `json:"policy_note,omitempty"`
	PlanFile             string `json:"-"`
	// Applies created from a confirmed plan point back to it.
	SourcePlanID string `json:"source_plan_id,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type OperationStore struct {
	db *sql.DB
}

func NewOperationStore(db *sql.DB) *OperationStore {
	return &OperationStore{db: db}
}

func (os *OperationStore) CreateTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS operations (
		id UUID PRIMARY KEY,
		repository_id UUID NOT NULL,
		user_id UUID NOT NULL,
		type VARCHAR(50) NOT NULL,
		status VARCHAR(50) NOT NULL DEFAULT 'pending',
		environment VARCHAR(50) NOT NULL,
		working_dir VARCHAR(255),
		commit_sha VARCHAR(40),
		plan TEXT,
		log TEXT,
		error TEXT,
		created_at TIMESTAMP DEFAULT NOW(),
		started_at TIMESTAMP,
		completed_at TIMESTAMP,
		FOREIGN KEY (repository_id) REFERENCES repositories(id),
		FOREIGN KEY (user_id) REFERENCES users(id)
	);

	CREATE INDEX IF NOT EXISTS idx_operations_repository_id ON operations(repository_id);
	CREATE INDEX IF NOT EXISTS idx_operations_user_id ON operations(user_id);
	CREATE INDEX IF NOT EXISTS idx_operations_status ON operations(status);
	CREATE INDEX IF NOT EXISTS idx_operations_created_at ON operations(created_at);
	`
	if _, err := os.db.Exec(query); err != nil {
		return err
	}

	// Additive migrations for databases created by older versions.
	for _, m := range []string{
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS commit_sha VARCHAR(40)`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS emergency BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS has_changes BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS awaiting_confirmation BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS policy_note TEXT`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS plan_file TEXT`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS source_plan_id UUID`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS application VARCHAR(63)`,
		// Instance names (app@group) can be longer than 63 characters.
		`ALTER TABLE operations ALTER COLUMN application TYPE VARCHAR(255)`,
		`ALTER TABLE operations ADD COLUMN IF NOT EXISTS source_commit_sha VARCHAR(40)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_application ON operations(repository_id, application)`,
	} {
		if _, err := os.db.Exec(m); err != nil {
			return err
		}
	}
	return nil
}

const operationColumns = `id, repository_id, user_id, type, status, environment, COALESCE(application, ''), COALESCE(working_dir, ''),
	COALESCE(commit_sha, ''), COALESCE(plan, ''), COALESCE(log, ''), COALESCE(error, ''),
	emergency, has_changes, awaiting_confirmation, COALESCE(policy_note, ''), COALESCE(plan_file, ''),
	COALESCE(source_plan_id::text, ''), created_at, started_at, completed_at,
	COALESCE(source_commit_sha, COALESCE(commit_sha, ''))`

func scanOperation(row interface{ Scan(...any) error }) (*Operation, error) {
	op := &Operation{}
	err := row.Scan(&op.ID, &op.RepositoryID, &op.UserID, &op.Type, &op.Status, &op.Environment, &op.Application, &op.WorkingDir,
		&op.CommitSHA, &op.Plan, &op.Log, &op.Error,
		&op.Emergency, &op.HasChanges, &op.AwaitingConfirmation, &op.PolicyNote, &op.PlanFile,
		&op.SourcePlanID, &op.CreatedAt, &op.StartedAt, &op.CompletedAt, &op.SourceCommitSHA)
	if err != nil {
		return nil, err
	}
	return op, nil
}

// FailInterrupted marks operations that were still pending or running when
// TowerCrane stopped: their tofu process is gone, so they can never finish.
func (os *OperationStore) FailInterrupted() (int64, error) {
	res, err := os.db.Exec(`UPDATE operations SET status = 'failed', awaiting_confirmation = FALSE,
		error = 'Interrupted: TowerCrane restarted while this operation was running', completed_at = $1
		WHERE status IN ('pending', 'running')`, time.Now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (os *OperationStore) Create(op *Operation) error {
	op.ID = uuid.New().String()
	op.CreatedAt = time.Now()
	_, err := os.db.Exec(`
	INSERT INTO operations (id, repository_id, user_id, type, status, environment, working_dir, log,
		emergency, source_plan_id, commit_sha, created_at, application)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::uuid, NULLIF($11, ''), $12, $13)`,
		op.ID, op.RepositoryID, op.UserID, op.Type, op.Status, op.Environment, op.WorkingDir, op.Log,
		op.Emergency, op.SourcePlanID, op.CommitSHA, op.CreatedAt, op.Application)
	return err
}

func (os *OperationStore) GetByID(id string) (*Operation, error) {
	return scanOperation(os.db.QueryRow(`SELECT `+operationColumns+` FROM operations WHERE id = $1`, id))
}

func (os *OperationStore) ListByRepository(repositoryID string, limit int, offset int) ([]*Operation, error) {
	rows, err := os.db.Query(`SELECT `+operationColumns+` FROM operations
		WHERE repository_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, repositoryID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	operations := []*Operation{}
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	return operations, rows.Err()
}

// GetLastSuccessfulApply returns the most recent successful apply of an
// Application: the baseline commit its policy is evaluated against.
// Emergency applies don't count: they skipped the policy, so they can't move
// its baseline. Returns sql.ErrNoRows if the Application was never applied.
func (os *OperationStore) GetLastSuccessfulApply(repositoryID, application string) (*Operation, error) {
	return scanOperation(os.db.QueryRow(`SELECT `+operationColumns+` FROM operations
		WHERE repository_id = $1 AND application = $2 AND type = 'apply' AND status = 'success'
			AND commit_sha IS NOT NULL AND NOT emergency
		ORDER BY created_at DESC LIMIT 1`, repositoryID, application))
}

// GetLastApplyOrDestroy returns the Application's latest successful apply
// or destroy: whether its resources currently exist (apply) or not
// (destroy). Emergency operations count: they really changed the cloud.
// Returns sql.ErrNoRows if there is none.
func (os *OperationStore) GetLastApplyOrDestroy(repositoryID, application string) (*Operation, error) {
	return scanOperation(os.db.QueryRow(`SELECT `+operationColumns+` FROM operations
		WHERE repository_id = $1 AND application = $2 AND type IN ('apply', 'destroy') AND status = 'success'
		ORDER BY COALESCE(completed_at, created_at) DESC LIMIT 1`, repositoryID, application))
}

// GetLatestByApplication returns an Application's most recent operation
// (used for the map's sync status badge).
func (os *OperationStore) GetLatestByApplication(repositoryID, application string) (*Operation, error) {
	return scanOperation(os.db.QueryRow(`SELECT `+operationColumns+` FROM operations
		WHERE repository_id = $1 AND application = $2 ORDER BY created_at DESC LIMIT 1`, repositoryID, application))
}

// SetLocation records where an operation runs once its Application was
// resolved at the commit being used.
func (os *OperationStore) SetLocation(id, workingDir, environment string) error {
	_, err := os.db.Exec(`UPDATE operations SET working_dir = $1, environment = $2 WHERE id = $3`, workingDir, environment, id)
	return err
}

func (os *OperationStore) UpdateCommit(id, sha string) error {
	_, err := os.db.Exec(`UPDATE operations SET commit_sha = $1 WHERE id = $2`, sha, id)
	return err
}

// UpdateSourceCommit records the commit of the Application's code.
func (os *OperationStore) UpdateSourceCommit(id, sha string) error {
	_, err := os.db.Exec(`UPDATE operations SET source_commit_sha = $1 WHERE id = $2`, sha, id)
	return err
}

func (os *OperationStore) UpdateStatus(id string, status string) error {
	_, err := os.db.Exec(`UPDATE operations SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (os *OperationStore) UpdateLog(id string, log string) error {
	_, err := os.db.Exec(`UPDATE operations SET log = $1 WHERE id = $2`, log, id)
	return err
}

func (os *OperationStore) UpdatePlan(id string, plan string) error {
	_, err := os.db.Exec(`UPDATE operations SET plan = $1 WHERE id = $2`, plan, id)
	return err
}

// SetPlanResult records what a successful plan found and what the policy
// decided. A plan that awaits confirmation supersedes any older plan of the
// same environment that was still waiting.
func (os *OperationStore) SetPlanResult(op *Operation) error {
	tx, err := os.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if op.AwaitingConfirmation {
		if _, err := tx.Exec(`UPDATE operations SET awaiting_confirmation = FALSE,
			policy_note = 'Superseded by a newer plan'
			WHERE repository_id = $1 AND application = $2 AND awaiting_confirmation AND id <> $3`,
			op.RepositoryID, op.Application, op.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE operations SET has_changes = $1, awaiting_confirmation = $2,
		policy_note = NULLIF($3, ''), plan_file = NULLIF($4, '') WHERE id = $5`,
		op.HasChanges, op.AwaitingConfirmation, op.PolicyNote, op.PlanFile, op.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimConfirmation atomically takes a plan out of the "awaiting
// confirmation" state, so two people clicking "yes" apply it only once.
func (os *OperationStore) ClaimConfirmation(planID, note string) (bool, error) {
	res, err := os.db.Exec(`UPDATE operations SET awaiting_confirmation = FALSE, policy_note = $1
		WHERE id = $2 AND type = 'plan' AND awaiting_confirmation`, note, planID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (os *OperationStore) Complete(id string, status string, log string, errorMsg string) error {
	_, err := os.db.Exec(`UPDATE operations SET status = $1, log = $2, error = $3, completed_at = $4 WHERE id = $5`,
		status, log, errorMsg, time.Now(), id)
	return err
}

func (os *OperationStore) Start(id string) error {
	_, err := os.db.Exec(`UPDATE operations SET status = 'running', started_at = $1 WHERE id = $2`, time.Now(), id)
	return err
}
