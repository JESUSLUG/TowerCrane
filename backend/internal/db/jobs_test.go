package db

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Needs a Postgres: TOWERCRANE_TEST_DB=1 with DB_* pointing at a test database.
func TestJobStore(t *testing.T) {
	if os.Getenv("TOWERCRANE_TEST_DB") == "" {
		t.Skip("set TOWERCRANE_TEST_DB=1 and DB_* to run against a test database")
	}
	database, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s := NewJobStore(database)
	if err := s.CreateTables(); err != nil {
		t.Fatal(err)
	}
	repo := uuid.New().String()
	defer database.Exec(`DELETE FROM job_runs WHERE repository_id = $1`, repo)
	defer database.Exec(`DELETE FROM job_schedules WHERE repository_id = $1`, repo)

	first := time.Now().Truncate(time.Second)
	got, err := s.CheckedAt(repo, "nightly", "apply", first)
	if err != nil || !got.Equal(first) {
		t.Fatalf("first CheckedAt = %v (want %v), %v", got, first, err)
	}
	later := first.Add(time.Hour)
	if got, _ := s.CheckedAt(repo, "nightly", "apply", later); !got.Equal(first) {
		t.Errorf("CheckedAt must keep the first time until SetCheckedAt, got %v", got)
	}
	s.SetCheckedAt(repo, "nightly", "apply", later)
	if got, _ := s.CheckedAt(repo, "nightly", "apply", later.Add(time.Hour)); !got.Equal(later) {
		t.Errorf("after SetCheckedAt got %v", got)
	}

	run := &JobRun{RepositoryID: repo, Job: "nightly", Application: "batch@prod", Action: "apply", Trigger: "manual", Status: JobRunRunning}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if busy, _ := s.Running(repo, "nightly"); !busy {
		t.Error("a running run must be reported")
	}
	s.FinishRun(run.ID, JobRunSuccess, "")
	runs, err := s.ListRuns(repo, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != JobRunSuccess || runs[0].FinishedAt == nil {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	if busy, _ := s.Running(repo, "nightly"); busy {
		t.Error("a finished run is not running")
	}
}
