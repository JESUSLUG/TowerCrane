package manifest

import (
	"bytes"
	"fmt"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/schedule"
	"gopkg.in/yaml.v3"
)

// Job applies an ephemeral Application (lifecycle: job) at scheduled times
// or on demand, and destroys it at other times or a while after each apply.
//
//	apiVersion: towercrane.io/v1alpha1
//	kind: Job
//	metadata:
//	  name: lambda-job
//	spec:
//	  application: lambda-job
//	  group: prod-us-east-1
//	  timezone: America/Mexico_City
//	  apply:
//	    schedule: "0 9 * * 1-5"     # weekdays 9:00
//	  destroy:
//	    after: 1h                   # or schedule: "0 18 * * 1-5"
//	  manual: true                  # "Run now" / "Destroy now" (default true)
//
// Merging the Job is its authorization: its applies and destroys run
// without a "yes", still checked against the Application's policy.
type Job struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       JobSpec  `yaml:"spec" json:"spec"`

	File string `yaml:"-" json:"file"`

	applySchedule   *schedule.Schedule
	destroySchedule *schedule.Schedule
	destroyAfter    time.Duration
}

type JobSpec struct {
	Application string      `yaml:"application" json:"application"`
	Group       string      `yaml:"group" json:"group,omitempty"`
	Description string      `yaml:"description" json:"description,omitempty"`
	Timezone    string      `yaml:"timezone" json:"timezone,omitempty"`
	Apply       *JobApply   `yaml:"apply" json:"apply,omitempty"`
	Destroy     *JobDestroy `yaml:"destroy" json:"destroy,omitempty"`
	Manual      *bool       `yaml:"manual" json:"manual,omitempty"`
}

type JobApply struct {
	Schedule string `yaml:"schedule" json:"schedule"`
}

type JobDestroy struct {
	// Schedule destroys at fixed times; After destroys that long after each
	// apply (Go duration: 30m, 2h, 1h30m). Use one of them.
	Schedule string `yaml:"schedule" json:"schedule,omitempty"`
	After    string `yaml:"after" json:"after,omitempty"`
}

// MinDestroyAfter keeps "after" from destroying while the apply could still
// be running.
const MinDestroyAfter = 5 * time.Minute

// ParseJob reads and validates one Job manifest.
func ParseJob(file string, content []byte) (*Job, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	var j Job
	if err := dec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", file, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: only one manifest per file is supported", file)
	}
	j.File = file
	fail := func(format string, args ...any) (*Job, error) {
		return nil, fmt.Errorf("%s: "+format, append([]any{file}, args...)...)
	}
	switch {
	case j.APIVersion != APIVersion:
		return fail("apiVersion must be %q", APIVersion)
	case j.Kind != KindJob:
		return fail("kind must be %q", KindJob)
	case !nameRe.MatchString(j.Metadata.Name):
		return fail("metadata.name must be lowercase letters, digits and dashes (max 63)")
	case !nameRe.MatchString(j.Spec.Application):
		return fail("spec.application must be an Application name")
	case j.Spec.Group != "" && !nameRe.MatchString(j.Spec.Group):
		return fail("spec.group: invalid group name %q", j.Spec.Group)
	}
	if j.Spec.Apply != nil {
		if j.Spec.Apply.Schedule == "" {
			return fail("spec.apply.schedule is required (or remove spec.apply to apply only with Run now)")
		}
		s, err := schedule.Parse(j.Spec.Apply.Schedule, j.Spec.Timezone)
		if err != nil {
			return fail("spec.apply.schedule: %v", err)
		}
		j.applySchedule = s
	}
	if d := j.Spec.Destroy; d != nil {
		switch {
		case d.Schedule != "" && d.After != "":
			return fail("spec.destroy: use either schedule or after, not both")
		case d.Schedule != "":
			s, err := schedule.Parse(d.Schedule, j.Spec.Timezone)
			if err != nil {
				return fail("spec.destroy.schedule: %v", err)
			}
			j.destroySchedule = s
		case d.After != "":
			after, err := time.ParseDuration(d.After)
			if err != nil || after < MinDestroyAfter {
				return fail("spec.destroy.after must be a duration of at least %s (e.g. 30m, 2h)", MinDestroyAfter)
			}
			j.destroyAfter = after
		default:
			return fail("spec.destroy needs schedule or after")
		}
	}
	if j.Spec.Apply == nil && !j.ManualEnabled() {
		return fail("a Job needs spec.apply.schedule or manual: true, or it never runs")
	}
	if j.Spec.Timezone != "" && j.applySchedule == nil && j.destroySchedule == nil {
		if _, err := schedule.Parse("* * * * *", j.Spec.Timezone); err != nil {
			return fail("spec.timezone: %v", err)
		}
	}
	return &j, nil
}

// Instance is the Application instance the Job runs ("app" or "app@group").
func (j *Job) Instance() string { return InstanceName(j.Spec.Application, j.Spec.Group) }

// ManualEnabled: "Run now" / "Destroy now" are allowed (default true).
func (j *Job) ManualEnabled() bool { return j.Spec.Manual == nil || *j.Spec.Manual }

func (j *Job) ApplySchedule() *schedule.Schedule   { return j.applySchedule }
func (j *Job) DestroySchedule() *schedule.Schedule { return j.destroySchedule }
func (j *Job) DestroyAfter() time.Duration         { return j.destroyAfter }
