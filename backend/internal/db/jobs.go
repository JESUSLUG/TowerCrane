package db

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Job runs: every apply or destroy a Job started (on schedule, after a
// delay, on demand, or catching up after TowerCrane was down), linked to the
// Operation that did it (logs, plan, result).

const (
	JobRunRunning = "running"
	JobRunSuccess = "success"
	JobRunFailed  = "failed"
	JobRunSkipped = "skipped"
)

type JobRun struct {
	ID           string     `json:"id"`
	RepositoryID string     `json:"repository_id"`
	Job          string     `json:"job"`
	Application  string     `json:"application"`
	Action       string     `json:"action"`  // apply, destroy
	Trigger      string     `json:"trigger"` // schedule, after, manual, catch-up
	Status       string     `json:"status"`  // running, success, failed, skipped
	OperationID  string     `json:"operation_id,omitempty"`
	TriggeredBy  string     `json:"triggered_by,omitempty"`
	Message      string     `json:"message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

type JobStore struct {
	db *sql.DB
}

func NewJobStore(db *sql.DB) *JobStore { return &JobStore{db: db} }

func (s *JobStore) CreateTables() error {
	_, err := s.db.Exec(`
	-- When the scheduler last evaluated each schedule of a Job, so a time
	-- fires once and a restart doesn't replay the past.
	CREATE TABLE IF NOT EXISTS job_schedules (
		repository_id UUID NOT NULL,
		job VARCHAR(255) NOT NULL,
		action VARCHAR(10) NOT NULL,
		checked_at TIMESTAMP NOT NULL,
		PRIMARY KEY (repository_id, job, action)
	);
	CREATE TABLE IF NOT EXISTS job_runs (
		id UUID PRIMARY KEY,
		repository_id UUID NOT NULL,
		job VARCHAR(255) NOT NULL,
		application VARCHAR(255) NOT NULL,
		action VARCHAR(10) NOT NULL,
		trigger VARCHAR(20) NOT NULL,
		status VARCHAR(20) NOT NULL,
		operation_id UUID,
		triggered_by UUID,
		message TEXT,
		created_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_job_runs_job ON job_runs(repository_id, job, created_at DESC);
	`)
	return err
}

// FailInterrupted marks runs left running by a previous process as failed.
func (s *JobStore) FailInterrupted() (int64, error) {
	res, err := s.db.Exec(`UPDATE job_runs SET status = $1, message = 'Interrupted: TowerCrane restarted while it ran',
		finished_at = $2 WHERE status = $3`, JobRunFailed, time.Now().UTC(), JobRunRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CheckedAt returns when a Job's schedule was last evaluated; the first
// time, it records now (schedules start from when TowerCrane first sees
// the Job, never from the past).
func (s *JobStore) CheckedAt(repoID, job, action string, now time.Time) (time.Time, error) {
	now = now.UTC() // TIMESTAMP columns are read back as UTC
	if _, err := s.db.Exec(`INSERT INTO job_schedules (repository_id, job, action, checked_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, repoID, job, action, now); err != nil {
		return time.Time{}, err
	}
	var t time.Time
	err := s.db.QueryRow(`SELECT checked_at FROM job_schedules WHERE repository_id = $1 AND job = $2 AND action = $3`,
		repoID, job, action).Scan(&t)
	return t, err
}

func (s *JobStore) SetCheckedAt(repoID, job, action string, t time.Time) error {
	_, err := s.db.Exec(`UPDATE job_schedules SET checked_at = $4 WHERE repository_id = $1 AND job = $2 AND action = $3`,
		repoID, job, action, t.UTC())
	return err
}

func (s *JobStore) CreateRun(r *JobRun) error {
	r.ID = uuid.New().String()
	r.CreatedAt = time.Now().UTC()
	_, err := s.db.Exec(`INSERT INTO job_runs (id, repository_id, job, application, action, trigger, status,
		operation_id, triggered_by, message, created_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid, NULLIF($10, ''), $11, $12)`,
		r.ID, r.RepositoryID, r.Job, r.Application, r.Action, r.Trigger, r.Status, r.OperationID, r.TriggeredBy,
		r.Message, r.CreatedAt, r.FinishedAt)
	return err
}

func (s *JobStore) SetRunOperation(id, operationID string) error {
	_, err := s.db.Exec(`UPDATE job_runs SET operation_id = $2::uuid WHERE id = $1`, id, operationID)
	return err
}

func (s *JobStore) FinishRun(id, status, message string) error {
	_, err := s.db.Exec(`UPDATE job_runs SET status = $2, message = NULLIF($3, ''), finished_at = $4 WHERE id = $1`,
		id, status, message, time.Now().UTC())
	return err
}

// Running reports whether a Job has a run in progress.
func (s *JobStore) Running(repoID, job string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM job_runs WHERE repository_id = $1 AND job = $2 AND status = $3`,
		repoID, job, JobRunRunning).Scan(&n)
	return n > 0, err
}

// ListRuns returns a repository's Job runs, newest first.
func (s *JobStore) ListRuns(repoID string, limit int) ([]*JobRun, error) {
	rows, err := s.db.Query(`SELECT id, repository_id, job, application, action, trigger, status,
		COALESCE(operation_id::text, ''), COALESCE(triggered_by::text, ''), COALESCE(message, ''), created_at, finished_at
		FROM job_runs WHERE repository_id = $1 ORDER BY created_at DESC LIMIT $2`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*JobRun{}
	for rows.Next() {
		r := &JobRun{}
		if err := rows.Scan(&r.ID, &r.RepositoryID, &r.Job, &r.Application, &r.Action, &r.Trigger, &r.Status,
			&r.OperationID, &r.TriggeredBy, &r.Message, &r.CreatedAt, &r.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
