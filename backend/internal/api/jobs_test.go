package api

import (
	"testing"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
)

func testJob(t *testing.T, spec string) *manifest.Job {
	t.Helper()
	j, err := manifest.ParseJob("j.yaml", []byte("apiVersion: towercrane.io/v1alpha1\nkind: Job\nmetadata:\n  name: j\nspec:\n  application: batch\n  timezone: America/Mexico_City\n"+spec))
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestDecideJob(t *testing.T) {
	mx, _ := time.LoadLocation("America/Mexico_City")
	at := func(h, m int) time.Time { return time.Date(2026, 11, 19, h, m, 0, 0, mx) } // a Thursday

	sched := testJob(t, "  apply: {schedule: \"0 9 * * 1-5\"}\n  destroy: {schedule: \"0 18 * * 1-5\"}\n")
	cases := []struct {
		name    string
		st      jobState
		now     time.Time
		action  string
		trigger string
		missed  bool
	}{
		{"before 9: nothing", jobState{applyChecked: at(8, 0), destroyChecked: at(8, 0)}, at(8, 59), "", "", false},
		{"9:00 applies", jobState{applyChecked: at(8, 59), destroyChecked: at(8, 59)}, at(9, 0), "apply", "schedule", false},
		{"9:05 still applies (grace)", jobState{applyChecked: at(8, 59), destroyChecked: at(8, 59)}, at(9, 5), "apply", "schedule", false},
		{"11:00 apply missed", jobState{applyChecked: at(8, 0), destroyChecked: at(8, 0)}, at(11, 0), "", "", true},
		{"already checked: no second apply", jobState{applyChecked: at(9, 0), destroyChecked: at(9, 0)}, at(9, 1), "", "", false},
		{"18:00 destroys when up", jobState{up: true, upSince: at(9, 1), applyChecked: at(17, 59), destroyChecked: at(17, 59)}, at(18, 0), "destroy", "schedule", false},
		{"18:00 nothing when down", jobState{applyChecked: at(17, 59), destroyChecked: at(17, 59)}, at(18, 0), "", "", false},
		{"late destroy catches up", jobState{up: true, upSince: at(9, 1), applyChecked: at(17, 0), destroyChecked: at(17, 0)}, at(21, 0), "destroy", "catch-up", false},
	}
	for _, c := range cases {
		d := decideJob(sched, c.st, c.now)
		if d.action != c.action || d.trigger != c.trigger || !d.missedApply.IsZero() != c.missed {
			t.Errorf("%s: got %+v", c.name, d)
		}
	}

	after := testJob(t, "  destroy: {after: 1h}\n")
	if d := decideJob(after, jobState{up: true, upSince: at(9, 0)}, at(9, 59)); d.action != "" {
		t.Errorf("before 1h: %+v", d)
	}
	if d := decideJob(after, jobState{up: true, upSince: at(9, 0)}, at(10, 0)); d.action != "destroy" || d.trigger != "after" {
		t.Errorf("after 1h: %+v", d)
	}
	if d := decideJob(after, jobState{up: false}, at(23, 0)); d.action != "" {
		t.Errorf("down: nothing to destroy: %+v", d)
	}
}
