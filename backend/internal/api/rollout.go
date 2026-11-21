package api

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

// RolloutEngine runs the Applications of a commit in dependency order:
//
//   - An Application starts (plan, then apply or wait for "yes") only once
//     every Application in its dependsOn was applied or had no changes:
//     its plan may need what they create (e.g. a secret before a service).
//   - Among Applications that are ready at the same time, the rollout order
//     decides (alphabetical by default, admins can reorder; dependsOn can
//     never be violated).
//   - If a dependency fails, is blocked or discarded, its dependents are
//     skipped. An admin can retry them.
//   - A paused Application doesn't start until resumed.
type RolloutEngine struct {
	ops       *OperationHandler
	store     *db.RolloutStore
	repoStore *db.RepositoryStore

	mu        sync.Mutex
	locks     map[string]*sync.Mutex
	advancing map[string]bool
}

func NewRolloutEngine(ops *OperationHandler, store *db.RolloutStore, repoStore *db.RepositoryStore) *RolloutEngine {
	e := &RolloutEngine{ops: ops, store: store, repoStore: repoStore, locks: map[string]*sync.Mutex{}, advancing: map[string]bool{}}
	ops.onPlanResolved = e.onPlanResolved
	return e
}

func (e *RolloutEngine) lock(rolloutID string) func() {
	e.mu.Lock()
	m, ok := e.locks[rolloutID]
	if !ok {
		m = &sync.Mutex{}
		e.locks[rolloutID] = m
	}
	e.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Start creates the rollout of a commit and runs it.
//
// reasons (from affectedApps) says which Applications to plan; the others
// start as "unchanged". nil plans every Application.
//
// codeHeads maps each Application to its code's commit (see appInputs).
func (e *RolloutEngine) Start(repo *models.Repository, sha string, apps []*manifest.Application,
	reasons map[string]planReason, codeHeads map[string]string) {
	if len(apps) == 0 {
		return
	}
	byName := map[string]*manifest.Application{}
	for _, a := range apps {
		byName[a.Metadata.Name] = a
	}
	r := &db.Rollout{RepositoryID: repo.ID, CommitSHA: sha}
	for i, name := range manifest.ApplyOrder(apps) {
		it := &db.RolloutItem{
			Application: name,
			Position:    i,
			DependsOn:   byName[name].Deps(),
			SourceSHA:   codeHeads[name],
		}
		if reasons != nil && reasons[name] == "" {
			it.Status, it.Message = db.ItemUnchanged, "Not planned: none of its files changed since it was last applied"
		}
		r.Items = append(r.Items, it)
	}
	if err := e.store.Create(r); err != nil {
		log.Printf("rollout: %s: failed to create rollout for %s: %v", repo.FullName, short(sha), err)
		return
	}
	e.Advance(r.ID)
}

// Advance starts every item that is ready, one at a time in rollout order,
// until nothing else can move. Plans run synchronously; an item waiting for
// a "yes" doesn't hold back items that don't depend on it.
func (e *RolloutEngine) Advance(rolloutID string) {
	// One advancer per rollout. A call while another one is running returns
	// right away: that advancer re-reads the rollout after each item, so it
	// sees whatever changed (a retry, a reorder, a resume).
	if !e.claimAdvance(rolloutID) {
		return
	}
	for {
		// The rollout lock is held only to decide and to record, never while
		// a plan runs, so retry/pause/reorder answer immediately.
		unlock := e.lock(rolloutID)
		repo, plan, it := e.startNext(rolloutID)
		if plan == nil {
			e.releaseAdvance(rolloutID) // under the lock: a later change starts a new advancer
			unlock()
			return
		}
		unlock()

		e.ops.runWith(plan, repo, runOptions{auto: true})

		unlock = e.lock(rolloutID)
		// Re-read: positions may have changed, and a "yes" or a discard may
		// already have moved the item on.
		if cur, err := e.item(rolloutID, it.Application); err == nil && cur.Status == db.ItemPlanning && cur.PlanOpID == plan.ID {
			e.recordPlanOutcome(cur)
			e.store.UpdateItem(cur)
		}
		unlock()
	}
}

func (e *RolloutEngine) claimAdvance(rolloutID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.advancing[rolloutID] {
		return false
	}
	e.advancing[rolloutID] = true
	return true
}

func (e *RolloutEngine) releaseAdvance(rolloutID string) {
	e.mu.Lock()
	delete(e.advancing, rolloutID)
	e.mu.Unlock()
}

// startNext skips what can't run any more and starts the plan of the next
// ready Application. Returns a nil plan when there is nothing to start.
// Called with the rollout lock held.
func (e *RolloutEngine) startNext(rolloutID string) (*models.Repository, *db.Operation, *db.RolloutItem) {
	for {
		r, err := e.store.Get(rolloutID)
		if err != nil {
			log.Printf("rollout %s: %v", rolloutID, err)
			return nil, nil, nil
		}
		if r.Status != "running" {
			return nil, nil, nil
		}
		repo, err := e.repoStore.GetRepositoryByID(r.RepositoryID)
		if err != nil {
			return nil, nil, nil
		}

		status := map[string]string{}
		for _, it := range r.Items {
			status[it.Application] = it.Status
		}

		var next *db.RolloutItem
		changed := false
		for _, it := range r.Items {
			if it.Status != db.ItemWaiting {
				continue
			}
			ready, failedDep := true, ""
			for _, d := range it.DependsOn {
				switch st := status[d]; {
				case db.ItemSucceeded(st):
				case db.ItemFinished(st):
					failedDep = d
				default:
					ready = false
				}
				if failedDep != "" {
					break
				}
			}
			if failedDep != "" {
				it.Status = db.ItemSkipped
				it.Message = fmt.Sprintf("Skipped: its dependency %s ended as %s", failedDep, strings.ReplaceAll(status[failedDep], "_", " "))
				e.store.UpdateItem(it)
				changed = true
				break // re-read: skipping may cascade
			}
			if ready && !it.Paused {
				next = it
				break
			}
		}
		if changed {
			continue
		}
		if next == nil {
			e.finishIfDone(r)
			return nil, nil, nil
		}
		if plan := e.startItem(repo, r, next); plan != nil {
			return repo, plan, next
		}
	}
}

// startItem creates the plan of one Application and marks it planning.
func (e *RolloutEngine) startItem(repo *models.Repository, r *db.Rollout, it *db.RolloutItem) *db.Operation {
	plan := &db.Operation{
		RepositoryID: repo.ID,
		UserID:       repo.ConnectedBy,
		Type:         "plan",
		Status:       "pending",
		Application:  it.Application,
		Environment:  it.Application,
		Log:          fmt.Sprintf("Rollout of %s (position %d)", short(r.CommitSHA), it.Position+1),
	}
	if err := e.ops.operationStore.Create(plan); err != nil {
		it.Status, it.Message = db.ItemFailed, "Failed to create the plan: "+err.Error()
		e.store.UpdateItem(it)
		return nil
	}
	it.Status, it.PlanOpID, it.Message = db.ItemPlanning, plan.ID, ""
	e.store.UpdateItem(it)
	return plan
}

// recordPlanOutcome reads where a finished plan (and any automatic apply)
// left an item.
func (e *RolloutEngine) recordPlanOutcome(it *db.RolloutItem) {
	plan, err := e.ops.operationStore.GetByID(it.PlanOpID)
	if err != nil {
		it.Status, it.Message = db.ItemFailed, "Plan not found"
		return
	}
	if apply := e.applyOf(plan); apply != nil {
		it.ApplyOpID = apply.ID
		e.setFromApply(it, apply)
		return
	}
	switch {
	case plan.Status == "failed":
		it.Status, it.Message = db.ItemFailed, firstLine(plan.Error)
	case !plan.HasChanges:
		it.Status, it.Message = db.ItemNoChanges, ""
	case plan.AwaitingConfirmation:
		it.Status, it.Message = db.ItemAwaitingYes, firstLines(plan.PolicyNote, 2)
	case strings.HasPrefix(plan.PolicyNote, "Rejected"):
		it.Status, it.Message = db.ItemRejected, firstLines(plan.PolicyNote, 2)
	default:
		it.Status, it.Message = db.ItemBlocked, firstLine(plan.PolicyNote)
	}
}

// applyOf finds the apply created from a plan, if any.
func (e *RolloutEngine) applyOf(plan *db.Operation) *db.Operation {
	ops, err := e.ops.operationStore.ListByRepository(plan.RepositoryID, 50, 0)
	if err != nil {
		return nil
	}
	for _, op := range ops {
		if op.Type == "apply" && op.SourcePlanID == plan.ID {
			return op
		}
	}
	return nil
}

func (e *RolloutEngine) setFromApply(it *db.RolloutItem, apply *db.Operation) {
	switch apply.Status {
	case "success":
		it.Status, it.Message = db.ItemApplied, ""
	case "failed":
		it.Status, it.Message = db.ItemFailed, firstLine(apply.Error)
	default:
		it.Status, it.Message = db.ItemApplying, ""
	}
}

// onPlanResolved is called when someone confirms or discards a plan that
// was waiting for a "yes", once the apply (if any) finished.
func (e *RolloutEngine) onPlanResolved(planID string, apply *db.Operation) {
	rolloutID, app, err := e.store.FindByPlan(planID)
	if err != nil {
		return // not part of a rollout (manual plan)
	}
	// Asynchronous: the rollout may be busy planning another Application,
	// and the caller can be an HTTP request.
	go func() {
		unlock := e.lock(rolloutID)
		r, err := e.store.Get(rolloutID)
		if err == nil {
			for _, it := range r.Items {
				if it.Application != app {
					continue
				}
				switch {
				case apply == nil:
					it.Status, it.Message = db.ItemDiscarded, "The plan was discarded"
				case db.ItemFinished(it.Status) && apply.Status != "success" && apply.Status != "failed":
					// A late "applying" notice must not undo a final status.
				default:
					it.ApplyOpID = apply.ID
					e.setFromApply(it, apply)
				}
				e.store.UpdateItem(it)
			}
		}
		unlock()
		e.Advance(rolloutID)
	}()
}

func (e *RolloutEngine) finishIfDone(r *db.Rollout) {
	for _, it := range r.Items {
		if !db.ItemFinished(it.Status) {
			return
		}
	}
	e.store.SetStatus(r.ID, "done")
}

// Reorder changes the order of a running rollout. Applications that already
// started keep their place in the history; the order only matters for the
// ones still waiting. dependsOn can't be violated.
func (e *RolloutEngine) Reorder(rolloutID string, order []string) error {
	unlock := e.lock(rolloutID)
	r, err := e.store.Get(rolloutID)
	if err != nil {
		unlock()
		return err
	}
	if r.Status != "running" {
		unlock()
		return fmt.Errorf("this rollout is %s", r.Status)
	}
	apps := make([]*manifest.Application, 0, len(r.Items))
	for _, it := range r.Items {
		a := &manifest.Application{}
		a.Metadata.Name = it.Application
		a.Spec.SyncPolicy.DependsOn = it.DependsOn
		apps = append(apps, a)
	}
	if err := manifest.ValidOrder(apps, order); err != nil {
		unlock()
		return err
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	for _, it := range r.Items {
		it.Position = pos[it.Application]
		e.store.UpdateItem(it)
	}
	unlock()
	go e.Advance(rolloutID)
	return nil
}

// SetPaused pauses or resumes a waiting Application.
func (e *RolloutEngine) SetPaused(rolloutID, app string, paused bool) error {
	unlock := e.lock(rolloutID)
	it, err := e.item(rolloutID, app)
	if err != nil {
		unlock()
		return err
	}
	if it.Status != db.ItemWaiting {
		unlock()
		return fmt.Errorf("%s already started (%s)", app, it.Status)
	}
	it.Paused = paused
	e.store.UpdateItem(it)
	unlock()
	if !paused {
		go e.Advance(rolloutID)
	}
	return nil
}

// Retry puts a failed, blocked, discarded or skipped Application (and the
// dependents skipped because of it) back in the queue.
func (e *RolloutEngine) Retry(rolloutID, app string) error {
	unlock := e.lock(rolloutID)
	r, err := e.store.Get(rolloutID)
	if err != nil {
		unlock()
		return err
	}
	if r.Status == "superseded" {
		unlock()
		return fmt.Errorf("a newer commit replaced this rollout")
	}
	var target *db.RolloutItem
	for _, it := range r.Items {
		if it.Application == app {
			target = it
		}
	}
	if target == nil {
		unlock()
		return fmt.Errorf("Application %q is not part of this rollout", app)
	}
	switch target.Status {
	case db.ItemFailed, db.ItemBlocked, db.ItemRejected, db.ItemDiscarded, db.ItemSkipped, db.ItemUnchanged:
	default:
		unlock()
		return fmt.Errorf("%s can't be retried while %s", app, strings.ReplaceAll(target.Status, "_", " "))
	}
	target.Status, target.Message, target.PlanOpID, target.ApplyOpID = db.ItemWaiting, "", "", ""
	e.store.UpdateItem(target)
	for _, it := range r.Items {
		if it.Status == db.ItemSkipped {
			it.Status, it.Message = db.ItemWaiting, ""
			e.store.UpdateItem(it)
		}
	}
	e.store.SetStatus(rolloutID, "running")
	unlock()
	go e.Advance(rolloutID)
	return nil
}

func (e *RolloutEngine) item(rolloutID, app string) (*db.RolloutItem, error) {
	r, err := e.store.Get(rolloutID)
	if err != nil {
		return nil, err
	}
	for _, it := range r.Items {
		if it.Application == app {
			return it, nil
		}
	}
	return nil, fmt.Errorf("Application %q is not part of this rollout", app)
}

// firstLines joins the first n non-empty lines (e.g. a title and its first
// reason).
func firstLines(s string, n int) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- ")); l != "" {
			out = append(out, l)
		}
		if len(out) == n {
			break
		}
	}
	return firstLine(strings.Join(out, " "))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}
