package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/gin-gonic/gin"
)

// Jobs (kind: Job) keep an ephemeral Application (lifecycle: job) up only
// when needed: TowerCrane applies it at scheduled times or on demand, and
// destroys it at other times or a while after each apply. Everything goes
// through the same plan/apply/destroy Operations as the rest of TowerCrane
// (logs, history, policy checks); the merged Job is the authorization, so
// its runs need no "yes".

const (
	jobTick = 30 * time.Second
	// An apply that fires this late (TowerCrane was down) is skipped: the
	// moment it was meant for has passed. A late destroy always runs, so
	// nothing keeps running (and costing) longer than intended.
	jobApplyGrace = 10 * time.Minute
)

type JobRunner struct {
	ops       *OperationHandler
	store     *db.JobStore
	repoStore *db.RepositoryStore
	snapshots *SnapshotStore
	gh        *GitHubAccess

	mu      sync.Mutex
	running map[string]bool // repoID/job
}

func NewJobRunner(ops *OperationHandler, store *db.JobStore, repoStore *db.RepositoryStore, snapshots *SnapshotStore, gh *GitHubAccess) *JobRunner {
	return &JobRunner{ops: ops, store: store, repoStore: repoStore, snapshots: snapshots, gh: gh, running: map[string]bool{}}
}

// loadJobs reads the Jobs of the commit TowerCrane last read for a repository.
func (r *JobRunner) loadJobs(ctx context.Context, repo *models.Repository) (manifest.LoadResult, error) {
	snap := r.snapshots.Get(repo.ID)
	if snap == nil {
		var err error
		if snap, err = r.snapshots.readSnapshot(ctx, r.gh, repo, ""); err != nil {
			return manifest.LoadResult{}, err
		}
	}
	return manifest.LoadIn(snap.Files, r.ops.projects.Layout(repo.ID)), nil
}

// Run starts an apply or destroy of a Job's Application. It refuses while
// another run of the same Job is in progress.
func (r *JobRunner) Run(repo *models.Repository, job *manifest.Job, action, trigger, userID string) (*db.JobRun, error) {
	key := repo.ID + "/" + job.Metadata.Name
	r.mu.Lock()
	if r.running[key] {
		r.mu.Unlock()
		return nil, fmt.Errorf("job %s is already running", job.Metadata.Name)
	}
	r.running[key] = true
	r.mu.Unlock()
	release := func() {
		r.mu.Lock()
		delete(r.running, key)
		r.mu.Unlock()
	}

	if userID == "" {
		userID = repo.ConnectedBy
	}
	run := &db.JobRun{
		RepositoryID: repo.ID, Job: job.Metadata.Name, Application: job.Instance(),
		Action: action, Trigger: trigger, Status: db.JobRunRunning, TriggeredBy: userID,
	}
	if err := r.store.CreateRun(run); err != nil {
		release()
		return nil, err
	}
	op := &db.Operation{
		RepositoryID: repo.ID,
		UserID:       userID,
		Type:         action,
		Status:       "pending",
		Application:  job.Instance(),
		Environment:  job.Instance(),
		Log:          fmt.Sprintf("Job %s: %s (%s)", job.Metadata.Name, action, trigger),
	}
	if err := r.ops.operationStore.Create(op); err != nil {
		r.store.FinishRun(run.ID, db.JobRunFailed, "Failed to create the operation: "+err.Error())
		release()
		return nil, err
	}
	r.store.SetRunOperation(run.ID, op.ID)
	run.OperationID = op.ID

	go func() {
		defer release()
		r.ops.runWith(op, repo, runOptions{job: true})
		status, msg := db.JobRunSuccess, ""
		if done, err := r.ops.operationStore.GetByID(op.ID); err != nil || done.Status != "success" {
			status = db.JobRunFailed
			if done != nil {
				msg = firstLine(done.Error)
			}
		}
		r.store.FinishRun(run.ID, status, msg)
		log.Printf("job %s/%s: %s (%s) %s", repo.FullName, job.Metadata.Name, action, trigger, status)
	}()
	return run, nil
}

// isUp reports whether the Application's resources exist: its latest
// successful apply/destroy was an apply. upSince is when.
func (r *JobRunner) isUp(repoID, instance string) (up bool, upSince time.Time) {
	op, err := r.ops.operationStore.GetLastApplyOrDestroy(repoID, instance)
	if err != nil || op.Type != "apply" {
		return false, time.Time{}
	}
	if op.CompletedAt != nil {
		return true, *op.CompletedAt
	}
	return true, op.CreatedAt
}

// Start runs the scheduler until ctx ends.
func (r *JobRunner) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(jobTick)
		defer t.Stop()
		for {
			r.tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func (r *JobRunner) tick(ctx context.Context) {
	repos, err := r.repoStore.ListRepositories()
	if err != nil {
		log.Printf("jobs: %v", err)
		return
	}
	now := time.Now()
	for _, repo := range repos {
		loaded, err := r.loadJobs(ctx, repo)
		if err != nil {
			log.Printf("jobs: %s: %v", repo.FullName, err)
			continue
		}
		for _, job := range loaded.Jobs {
			r.evaluate(repo, job, now)
		}
	}
}

// jobState is what the scheduler knows about a Job at one moment.
type jobState struct {
	up             bool
	upSince        time.Time
	applyChecked   time.Time // last evaluation of spec.apply.schedule
	destroyChecked time.Time // last evaluation of spec.destroy.schedule
}

// jobDecision is what one scheduler pass does for a Job.
type jobDecision struct {
	action  string // "apply", "destroy" or "" (nothing to run)
	trigger string // schedule, after, catch-up
	// Which schedules were evaluated now (their checked time moves on).
	checkedApply, checkedDestroy bool
	// missedApply is set when a scheduled apply fired too late to run.
	missedApply time.Time
}

// decideJob is the scheduler's rule, pure so it can be tested:
//   - a scheduled destroy fires when due if the Application is up (late ones
//     still run, as "catch-up": nothing keeps running longer than meant);
//   - destroy.after fires once the Application has been up that long;
//   - a scheduled apply fires when due, unless it is more than
//     jobApplyGrace late (TowerCrane was down): then it is recorded as missed.
//
// Destroys win over applies in the same pass.
func decideJob(job *manifest.Job, st jobState, now time.Time) jobDecision {
	var d jobDecision
	if s := job.DestroySchedule(); s != nil {
		if next := s.Next(st.destroyChecked); !next.IsZero() && !next.After(now) {
			d.checkedDestroy = true
			if st.up {
				d.action, d.trigger = "destroy", "schedule"
				if now.Sub(next) > jobApplyGrace {
					d.trigger = "catch-up"
				}
				return d
			}
		}
	}
	if after := job.DestroyAfter(); after > 0 && st.up && now.Sub(st.upSince) >= after {
		d.action, d.trigger = "destroy", "after"
		if now.Sub(st.upSince) > after+jobApplyGrace {
			d.trigger = "catch-up"
		}
		return d
	}
	if s := job.ApplySchedule(); s != nil {
		if next := s.Next(st.applyChecked); !next.IsZero() && !next.After(now) {
			d.checkedApply = true
			if now.Sub(next) > jobApplyGrace {
				d.missedApply = next
				return d
			}
			d.action, d.trigger = "apply", "schedule"
		}
	}
	return d
}

// evaluate fires whatever is due for one Job.
func (r *JobRunner) evaluate(repo *models.Repository, job *manifest.Job, now time.Time) {
	name := job.Metadata.Name
	if busy, err := r.store.Running(repo.ID, name); err != nil || busy {
		return // try again on the next tick, nothing is lost
	}
	st := jobState{}
	st.up, st.upSince = r.isUp(repo.ID, job.Instance())
	var err error
	if job.DestroySchedule() != nil {
		if st.destroyChecked, err = r.store.CheckedAt(repo.ID, name, "destroy", now); err != nil {
			return
		}
	}
	if job.ApplySchedule() != nil {
		if st.applyChecked, err = r.store.CheckedAt(repo.ID, name, "apply", now); err != nil {
			return
		}
	}

	d := decideJob(job, st, now)
	if d.checkedDestroy {
		r.store.SetCheckedAt(repo.ID, name, "destroy", now)
	}
	if d.checkedApply {
		r.store.SetCheckedAt(repo.ID, name, "apply", now)
	}
	if !d.missedApply.IsZero() {
		finished := now.UTC()
		r.store.CreateRun(&db.JobRun{
			RepositoryID: repo.ID, Job: name, Application: job.Instance(), Action: "apply", Trigger: "schedule",
			Status: db.JobRunSkipped, FinishedAt: &finished,
			Message: fmt.Sprintf("Missed: TowerCrane was not running at %s",
				d.missedApply.In(job.ApplySchedule().Location).Format("2006-01-02 15:04 MST")),
		})
	}
	if d.action != "" {
		r.fire(repo, job, d.action, d.trigger)
	}
}

func (r *JobRunner) fire(repo *models.Repository, job *manifest.Job, action, trigger string) {
	if _, err := r.Run(repo, job, action, trigger, ""); err != nil {
		log.Printf("jobs: %s/%s: %v", repo.FullName, job.Metadata.Name, err)
	}
}

// --- API ----------------------------------------------------------------

type jobView struct {
	Name        string       `json:"name"`
	File        string       `json:"file"`
	Application string       `json:"application"` // instance
	Group       string       `json:"group,omitempty"`
	Description string       `json:"description,omitempty"`
	Timezone    string       `json:"timezone"`
	Apply       string       `json:"apply_schedule,omitempty"`
	Destroy     string       `json:"destroy_schedule,omitempty"`
	After       string       `json:"destroy_after,omitempty"`
	Manual      bool         `json:"manual"`
	Up          bool         `json:"up"`
	UpSince     *time.Time   `json:"up_since,omitempty"`
	NextApply   *time.Time   `json:"next_apply,omitempty"`
	NextDestroy *time.Time   `json:"next_destroy,omitempty"`
	Running     bool         `json:"running"`
	Runs        []*db.JobRun `json:"runs"`
}

// List returns a repository's Jobs with their state, next times and
// recent runs.
func (r *JobRunner) List(c *gin.Context) {
	repo, err := r.repoStore.GetRepositoryByID(c.Param("repo_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	loaded, err := r.loadJobs(c.Request.Context(), repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	runs, err := r.store.ListRuns(repo.ID, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list job runs"})
		return
	}
	now := time.Now()
	views := make([]jobView, 0, len(loaded.Jobs))
	for _, j := range loaded.Jobs {
		tz := j.Spec.Timezone
		if tz == "" {
			tz = "UTC"
		}
		v := jobView{
			Name: j.Metadata.Name, File: j.File, Application: j.Instance(), Group: j.Spec.Group,
			Description: j.Spec.Description, Timezone: tz, Manual: j.ManualEnabled(), Runs: []*db.JobRun{},
		}
		if s := j.ApplySchedule(); s != nil {
			v.Apply = s.Expr
			if next := s.Next(now); !next.IsZero() {
				v.NextApply = &next
			}
		}
		up, since := r.isUp(repo.ID, j.Instance())
		v.Up = up
		if up {
			v.UpSince = &since
		}
		if s := j.DestroySchedule(); s != nil {
			v.Destroy = s.Expr
			if next := s.Next(now); !next.IsZero() {
				v.NextDestroy = &next
			}
		}
		if after := j.DestroyAfter(); after > 0 {
			v.After = after.String()
			if up {
				at := since.Add(after)
				v.NextDestroy = &at
			}
		}
		for _, run := range runs {
			if run.Job != j.Metadata.Name {
				continue
			}
			if run.Status == db.JobRunRunning {
				v.Running = true
			}
			if len(v.Runs) < 20 {
				v.Runs = append(v.Runs, run)
			}
		}
		views = append(views, v)
	}
	c.JSON(http.StatusOK, gin.H{"jobs": views})
}

type jobRunRequest struct {
	Action string `json:"action" binding:"required"` // apply or destroy
}

// RunNow applies or destroys a Job's Application right away ("Run now" /
// "Destroy now"), when the Job allows manual runs.
func (r *JobRunner) RunNow(c *gin.Context) {
	var req jobRunRequest
	if err := c.ShouldBindJSON(&req); err != nil || (req.Action != "apply" && req.Action != "destroy") {
		c.JSON(http.StatusBadRequest, gin.H{"error": `action must be "apply" or "destroy"`})
		return
	}
	repo, err := r.repoStore.GetRepositoryByID(c.Param("repo_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	loaded, err := r.loadJobs(c.Request.Context(), repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	job := loaded.JobByName(c.Param("job"))
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if !job.ManualEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "this Job doesn't allow manual runs (spec.manual: false)"})
		return
	}
	run, err := r.Run(repo, job, req.Action, "manual", c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, run)
}
