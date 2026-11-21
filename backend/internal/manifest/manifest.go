// Package manifest reads TowerCrane manifests: declarative YAML files that
// live in the project's manifests folder (`.towercrane/` by default) and
// declare what TowerCrane manages and how, without touching the
// Terraform/OpenTofu code.
//
//	apiVersion: towercrane.io/v1alpha1
//	kind: Application
//	metadata:
//	  name: example-dev
//	spec:
//	  source:
//	    path: environments/dev
//	  destination:
//	    cloudAccount: floci-dev
//	  syncPolicy:
//	    apply: manual          # default for changes no rule matches
//	    dependsOn: [network]   # roll out after these Applications
//	    rules:                 # first match per planned change wins
//	      - when: {actions: [create], resources: ["aws_secretsmanager_secret.*"]}
//	        apply: auto
//	      - when: {actions: [delete]}
//	        apply: reject      # delete/replace are rejected by default anyway
//	  editScope:
//	    rules:
//	      - files: ["environments/dev/variables.tf"]
//	        blocks: ["variable.instance_count"]
//	        dependents: true
//	  variables:
//	    instance_count: 2
//	  display:
//	    showSource: true
//
// Groups say where an Application is applied. A Group manifest (any file in
// .towercrane/, e.g. .towercrane/groups/sandbox.yaml) names the cloud
// account of the group and the groups it is promoted after:
//
//	apiVersion: towercrane.io/v1alpha1
//	kind: Group
//	metadata:
//	  name: prod
//	spec:
//	  cloudAccount: aws-prod
//	  after: [sandbox]        # prod starts once sandbox applied cleanly
//
// An Application lists its groups (spec.groups, or spec.group for one).
// TowerCrane runs one instance per group, named <app>@<group>: same code,
// the group's cloud account, its own state (TOWERCRANE_GROUP is set for
// Terragrunt/OpenTofu), dependsOn resolved inside the same group ("vpc") or
// naming another group's instance ("vpc@general"). The same name may be
// used by different manifests in different groups (sandbox/ecr, prod/ecr).
//
// Parsing is strict: unknown fields, a wrong apiVersion/kind or invalid
// values are errors, so a typo can never silently widen what TowerCrane may
// change.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "towercrane.io/v1alpha1"
	Kind       = "Application"
	KindGroup  = "Group"
	// KindDiagram is how a group is drawn in TowerCrane: sections, colors
	// and visual links. Purely visual: it is not policy (changing it needs
	// no approval) and it never changes the apply order.
	KindDiagram = "Diagram"
	// KindJob applies an Application at scheduled times (or on demand) and
	// destroys it later: the lifecycle of something ephemeral, through the
	// same plan/apply/destroy as everything else.
	KindJob = "Job"

	// LifecycleJob marks an Application that only exists while its Job
	// keeps it up: rollouts don't apply it on merge, and its Job may
	// destroy it.
	LifecycleJob = "job"
	// Dir is where manifests live in a repository.
	Dir = ".towercrane"
)

// Plan actions a manifest can deny.
const (
	ActionCreate  = "create"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionReplace = "replace"
)

// Apply decisions, from least to most restrictive.
const (
	ApplyAuto   = "auto"   // apply without asking
	ApplyManual = "manual" // wait for an approver's "yes"
	ApplyReject = "reject" // never apply this plan, not even with a "yes"
)

// Engines TowerCrane can run (Terragrunt is detected per directory).
const (
	EngineOpenTofu  = "opentofu"
	EngineTerraform = "terraform"
)

// ImplicitRule always comes last: unless an Application's own rules say
// otherwise, plans that delete or replace something are rejected.
var ImplicitRule = Rule{When: When{Actions: []string{ActionDelete, ActionReplace}}, Apply: ApplyReject}

type Application struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`

	// File is the repository-relative path of the manifest (not YAML).
	File string `yaml:"-" json:"file"`
	// Hash identifies the exact manifest content (approvals bind to it).
	// For a group instance it covers the Group manifest too, so changing
	// the group (e.g. its cloud account) is a policy change as well.
	Hash string `yaml:"-" json:"hash"`

	// Set by Load. For a group instance Metadata.Name is "<App>@<Group>".
	// App is the manifest's metadata.name.
	App string `yaml:"-" json:"app"`
	// Group is the group of this instance ("" when the Application has no
	// groups) and GroupFile its manifest.
	Group     string `yaml:"-" json:"group,omitempty"`
	GroupFile string `yaml:"-" json:"groupFile,omitempty"`
	// CloudAccount is the cloud account the instance runs with: its
	// group's, or spec.destination.cloudAccount without groups.
	CloudAccount string `yaml:"-" json:"cloudAccount,omitempty"`
	// Promotion lists the instances of earlier groups (Group spec.after)
	// that must succeed first.
	Promotion []string `yaml:"-" json:"promotion,omitempty"`
}

// Deps is everything an instance waits for in a rollout: its dependsOn
// (resolved inside its group) and its group promotion.
func (a *Application) Deps() []string {
	if len(a.Promotion) == 0 {
		return a.Spec.SyncPolicy.DependsOn
	}
	return append(append([]string{}, a.Spec.SyncPolicy.DependsOn...), a.Promotion...)
}

// InstanceName is how TowerCrane names an Application in a group.
func InstanceName(app, group string) string {
	if group == "" {
		return app
	}
	return app + "@" + group
}

// Group is a set of Application instances that share a cloud account.
type Group struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       GroupSpec `yaml:"spec" json:"spec"`

	File string `yaml:"-" json:"file"`
	Hash string `yaml:"-" json:"hash"`
}

type GroupSpec struct {
	// CloudAccount is the name of a cloud account attached to the
	// repository in TowerCrane.
	CloudAccount string `yaml:"cloudAccount" json:"cloudAccount"`
	// After lists groups whose instances (in the same rollout) must all
	// succeed before this group starts: promotion order.
	After       []string          `yaml:"after" json:"after,omitempty"`
	Description string            `yaml:"description" json:"description,omitempty"`
	Labels      map[string]string `yaml:"labels" json:"labels,omitempty"`
}

type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

type Spec struct {
	// Group / Groups: where the Application is applied (Group manifests).
	// Use one or the other.
	Group       string            `yaml:"group" json:"group,omitempty"`
	Groups      []string          `yaml:"groups" json:"groups,omitempty"`
	// Lifecycle "job": the Application is ephemeral, applied and destroyed
	// by its Job (kind: Job), never by a rollout.
	Lifecycle string `yaml:"lifecycle" json:"lifecycle,omitempty"`
	Source      Source            `yaml:"source" json:"source"`
	Destination Destination       `yaml:"destination" json:"destination"`
	SyncPolicy  SyncPolicy        `yaml:"syncPolicy" json:"syncPolicy"`
	EditScope   EditScope         `yaml:"editScope" json:"editScope"`
	Variables   map[string]any    `yaml:"variables" json:"variables,omitempty"`
	Display     Display           `yaml:"display" json:"display"`
	Labels      map[string]string `yaml:"labels" json:"labels,omitempty"`
}

type Source struct {
	// Repo is the repository holding the code ("owner/name"), when it isn't
	// the repository of the manifests. It must be a source repository of
	// the project in TowerCrane.
	Repo string `yaml:"repo" json:"repo,omitempty"`
	// Path is the directory TowerCrane runs in: a Terraform/OpenTofu root
	// module, or a Terragrunt unit (a directory with terragrunt.hcl).
	Path string `yaml:"path" json:"path"`
	// Engine is "opentofu" (default) or "terraform". With Terragrunt it is
	// the engine Terragrunt drives.
	Engine string `yaml:"engine" json:"engine,omitempty"`
}

type Destination struct {
	// CloudAccount is the name of a cloud account attached to the
	// repository. Only for Applications without groups (a group sets it).
	CloudAccount string `yaml:"cloudAccount" json:"cloudAccount,omitempty"`
}

type SyncPolicy struct {
	// Apply is the default decision for planned changes no rule matches:
	// "auto" (apply after merge) or "manual" (wait for a "yes"). Default
	// manual.
	Apply string `yaml:"apply" json:"apply"`
	// Rules decide per planned change, first match wins; the whole plan
	// gets the strictest decision (reject > manual > auto). ImplicitRule is
	// always appended last.
	Rules []Rule `yaml:"rules" json:"rules,omitempty"`
	// DependsOn lists Applications (same repository, by metadata.name) that
	// must be applied before this one in a rollout.
	DependsOn []string `yaml:"dependsOn" json:"dependsOn,omitempty"`
}

type EditScope struct {
	// All allows changing anything in the Application (still subject to
	// the plan guard).
	All   bool       `yaml:"all" json:"all"`
	Rules []EditRule `yaml:"rules" json:"rules,omitempty"`
}

// Rule decides how planned changes that match When are applied.
type Rule struct {
	When  When   `yaml:"when" json:"when"`
	Apply string `yaml:"apply" json:"apply"` // auto, manual or reject
}

// When matches a planned change. Empty lists match anything.
type When struct {
	// Actions: create, update, delete, replace.
	Actions []string `yaml:"actions" json:"actions,omitempty"`
	// Resources are plan address patterns with "*" wildcards, matched
	// against the full address (module.x.aws_s3_bucket.b) and against the
	// resource part alone (aws_s3_bucket.b). Instance keys are ignored.
	Resources []string `yaml:"resources" json:"resources,omitempty"`
}

// Matches reports whether a planned change matches.
func (w When) Matches(address, action string) bool {
	if len(w.Actions) > 0 && !contains(w.Actions, action) {
		return false
	}
	if len(w.Resources) == 0 {
		return true
	}
	addr := StripIndexes(address)
	short := addr
	if parts := strings.Split(addr, "."); len(parts) >= 2 {
		// Drop module path: module.a.module.b.aws_x.y -> aws_x.y
		i := 0
		for i+1 < len(parts) && parts[i] == "module" {
			i += 2
		}
		short = strings.Join(parts[i:], ".")
	}
	for _, p := range w.Resources {
		if MatchBlock(p, addr) || MatchBlock(p, short) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var indexRe = regexp.MustCompile(`\[[^\]]*\]`)

// StripIndexes removes instance keys from a plan address:
// module.a["x"].aws_instance.main[0] -> module.a.aws_instance.main.
func StripIndexes(address string) string { return indexRe.ReplaceAllString(address, "") }

// PlannedChange is one change of a plan, for Decide.
type PlannedChange struct {
	Address string
	Action  string // create, update, delete, replace
}

// Decision is what an Application's rules say about a plan.
type Decision struct {
	Apply string `json:"apply"` // auto, manual or reject
	// Reasons explains the changes that made the decision this strict.
	Reasons []string `json:"reasons,omitempty"`
}

var applyRank = map[string]int{ApplyAuto: 0, ApplyManual: 1, ApplyReject: 2}

// Decide applies the Application's rules to every planned change: the first
// matching rule (then ImplicitRule, then syncPolicy.apply) decides each
// change, and the plan gets the strictest decision.
func (a *Application) Decide(changes []PlannedChange) Decision {
	rules := append(append([]Rule{}, a.Spec.SyncPolicy.Rules...), ImplicitRule)
	dec := Decision{Apply: ApplyAuto}
	for _, c := range changes {
		if c.Action == "" {
			continue
		}
		apply, why := a.ApplyMode(), "syncPolicy.apply"
		for i, r := range rules {
			if r.When.Matches(c.Address, c.Action) {
				apply = r.Apply
				if i < len(a.Spec.SyncPolicy.Rules) {
					why = fmt.Sprintf("syncPolicy.rules[%d]", i)
				} else {
					why = "TowerCrane default (delete/replace are rejected unless a rule allows them)"
				}
				break
			}
		}
		reason := fmt.Sprintf("%s (%s): %s by %s", c.Address, c.Action, apply, why)
		switch {
		case applyRank[apply] > applyRank[dec.Apply]:
			dec.Apply, dec.Reasons = apply, []string{reason}
		case applyRank[apply] == applyRank[dec.Apply] && apply != ApplyAuto:
			dec.Reasons = append(dec.Reasons, reason)
		}
	}
	return dec
}

// CodeRepo returns the repository of the Application's code: its
// spec.source.repo, or configRepo (the repository of the manifests).
func (a *Application) CodeRepo(configRepo string) string {
	if a.Spec.Source.Repo != "" {
		return a.Spec.Source.Repo
	}
	return configRepo
}

// EngineName returns the configured engine (default opentofu).
func (a *Application) EngineName() string {
	if a.Spec.Source.Engine == EngineTerraform {
		return EngineTerraform
	}
	return EngineOpenTofu
}

// EditRule allows changes to some blocks of some files.
type EditRule struct {
	// Files are repository-relative globs ("*" within a directory, "**"
	// across directories), e.g. "modules/compute/*.tf".
	Files []string `yaml:"files" json:"files"`
	// Blocks are HCL block addresses that may change in those files, with
	// "*" wildcards: "variable.instance_count", "resource.aws_instance.*",
	// "module.compute", "local.tags", "output.*", or "*" for any block.
	// Non-HCL files (e.g. .tfvars) need "*".
	Blocks []string `yaml:"blocks" json:"blocks"`
	// Dependents also allows the plan to change resources that use the
	// allowed blocks, directly or through other blocks and modules.
	Dependents bool `yaml:"dependents" json:"dependents"`
}

type Display struct {
	// ShowSource shows the HCL source of the Application's blocks on the
	// resource map.
	ShowSource bool `yaml:"showSource" json:"showSource"`
}

// ApplyMode returns "auto" or "manual".
func (a *Application) ApplyMode() string {
	if a.Spec.SyncPolicy.Apply == "auto" {
		return "auto"
	}
	return "manual"
}

// Summary is a short human description of the policy, shown when a
// manifest change waits for approval.
func (a *Application) Summary() []string {
	out := []string{"path=" + a.Spec.Source.Path, "apply=" + a.ApplyMode()}
	if len(a.Spec.SyncPolicy.DependsOn) > 0 {
		out = append(out, "dependsOn="+strings.Join(a.Spec.SyncPolicy.DependsOn, ","))
	}
	if a.Spec.EditScope.All {
		out = append(out, "edit=all")
	} else {
		out = append(out, fmt.Sprintf("edit=%d rule(s)", len(a.Spec.EditScope.Rules)))
	}
	if len(a.Spec.SyncPolicy.Rules) > 0 {
		out = append(out, fmt.Sprintf("apply-rules=%d", len(a.Spec.SyncPolicy.Rules)))
	}
	if a.Spec.Source.Engine != "" {
		out = append(out, "engine="+a.Spec.Source.Engine)
	}
	if a.Spec.Source.Repo != "" {
		out = append(out, "repo="+a.Spec.Source.Repo)
	}
	if a.Group != "" {
		out = append(out, "group="+a.Group)
	}
	if a.CloudAccount != "" {
		out = append(out, "cloud="+a.CloudAccount)
	}
	return out
}

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	varNameRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	blockPatRe = regexp.MustCompile(`^[A-Za-z0-9_*.\-]+$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// IsManifestPath reports whether a repository path is a TowerCrane manifest
// in the default layout: a YAML file anywhere under .towercrane/.
func IsManifestPath(p string) bool { return DefaultLayout().Holds(p) }

func isYAML(p string) bool { return strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") }

// Layout says where a project keeps its manifests: Dir is a folder of the
// configuration repository (default ".towercrane"), or "" for the whole
// repository (a repository that only holds configuration).
type Layout struct {
	Dir string
}

// DefaultLayout keeps manifests under .towercrane/, apart from the code.
func DefaultLayout() Layout { return Layout{Dir: Dir} }

// NewLayout normalizes a manifests folder: "", "/" or "." mean the whole
// repository.
func NewLayout(dir string) (Layout, error) {
	d := strings.Trim(strings.TrimSpace(dir), "/")
	if d == "" || d == "." {
		return Layout{}, nil
	}
	if strings.Contains(d, `\`) || strings.Contains(d, "..") || path.Clean(d) != d {
		return Layout{}, fmt.Errorf("the manifests folder must be a plain folder of the repository, got %q", dir)
	}
	return Layout{Dir: d}, nil
}

// Root: the whole repository holds configuration.
func (l Layout) Root() bool { return l.Dir == "" }

// String is how the folder is shown ("/" for the whole repository).
func (l Layout) String() string {
	if l.Root() {
		return "/"
	}
	return l.Dir + "/"
}

// Holds reports whether a path may be a manifest: a YAML file under Dir, or,
// for the whole repository, outside hidden folders (.github/...).
func (l Layout) Holds(p string) bool {
	if !isYAML(p) {
		return false
	}
	if !l.Root() {
		return strings.HasPrefix(p, l.Dir+"/")
	}
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if seg != "." && strings.HasPrefix(seg, ".") {
			return false // hidden folders (.github/...) are never configuration
		}
	}
	return true
}

// isTowerCraneYAML reports whether content declares a TowerCrane apiVersion.
func isTowerCraneYAML(content []byte) bool {
	var head struct {
		APIVersion string `yaml:"apiVersion"`
	}
	_ = yaml.Unmarshal(content, &head)
	return head.APIVersion == APIVersion
}

// Diagram describes how TowerCrane draws a group's Applications.
//
//	apiVersion: towercrane.io/v1alpha1
//	kind: Diagram
//	metadata:
//	  name: sandbox
//	spec:
//	  group: sandbox            # whose Applications it draws ("" = without group)
//	  sections:
//	    - name: Network
//	      color: blue
//	      apps: [vpc, ecr]
//	  nodes:
//	    vpc: {color: green}
//	    ecr: {color: red, label: Registry}
//	  links:                    # visual only; dependsOn is drawn anyway
//	    - {from: secrets, to: servicio-1, label: reads the secret}
type Diagram struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       DiagramSpec `yaml:"spec" json:"spec"`

	File string `yaml:"-" json:"file"`
	Hash string `yaml:"-" json:"hash"`
}

type DiagramSpec struct {
	Group    string                 `yaml:"group" json:"group"`
	Sections []DiagramSection       `yaml:"sections" json:"sections"`
	Nodes    map[string]DiagramNode `yaml:"nodes" json:"nodes"`
	Links    []DiagramLink          `yaml:"links" json:"links"`
}

type DiagramSection struct {
	Name  string   `yaml:"name" json:"name"`
	Color string   `yaml:"color" json:"color,omitempty"`
	Apps  []string `yaml:"apps" json:"apps"`
}

type DiagramNode struct {
	Color string `yaml:"color" json:"color,omitempty"`
	Label string `yaml:"label" json:"label,omitempty"`
}

type DiagramLink struct {
	From  string `yaml:"from" json:"from"`
	To    string `yaml:"to" json:"to"`
	Label string `yaml:"label" json:"label,omitempty"`
}

// DiagramColors are the colors a Diagram may use.
var DiagramColors = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink", "gray"}

// ParseDiagram reads and validates one Diagram manifest.
func ParseDiagram(file string, content []byte) (*Diagram, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	var d Diagram
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", file, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: only one manifest per file is supported", file)
	}
	d.File = file
	sum := sha256.Sum256(content)
	d.Hash = "sha256:" + hex.EncodeToString(sum[:])
	fail := func(format string, args ...any) (*Diagram, error) {
		return nil, fmt.Errorf("%s: "+format, append([]any{file}, args...)...)
	}
	switch {
	case d.APIVersion != APIVersion:
		return fail("apiVersion must be %q", APIVersion)
	case d.Kind != KindDiagram:
		return fail("kind must be %q", KindDiagram)
	case !nameRe.MatchString(d.Metadata.Name):
		return fail("metadata.name must be lowercase letters, digits and dashes (max 63)")
	case d.Spec.Group != "" && !nameRe.MatchString(d.Spec.Group):
		return fail("spec.group: invalid group name %q", d.Spec.Group)
	}
	color := func(where, c string) error {
		if c != "" && !contains(DiagramColors, c) {
			return fmt.Errorf("%s: %s: unknown color %q (use one of %s)", file, where, c, strings.Join(DiagramColors, ", "))
		}
		return nil
	}
	inSection := map[string]string{}
	for i, sec := range d.Spec.Sections {
		where := fmt.Sprintf("spec.sections[%d]", i)
		if strings.TrimSpace(sec.Name) == "" {
			return fail("%s.name is required", where)
		}
		if err := color(where+".color", sec.Color); err != nil {
			return nil, err
		}
		for _, a := range sec.Apps {
			if !nameRe.MatchString(a) {
				return fail("%s.apps: invalid Application name %q", where, a)
			}
			if other, dup := inSection[a]; dup {
				return fail("%s.apps: %q is already in section %q", where, a, other)
			}
			inSection[a] = sec.Name
		}
	}
	for name, n := range d.Spec.Nodes {
		if !nameRe.MatchString(name) {
			return fail("spec.nodes: invalid Application name %q", name)
		}
		if err := color("spec.nodes."+name+".color", n.Color); err != nil {
			return nil, err
		}
	}
	for i, l := range d.Spec.Links {
		if !nameRe.MatchString(l.From) || !nameRe.MatchString(l.To) {
			return fail("spec.links[%d]: from and to must be Application names", i)
		}
	}
	return &d, nil
}

// kindOf reads only the kind of a manifest.
func kindOf(content []byte) string {
	var head struct {
		Kind string `yaml:"kind"`
	}
	_ = yaml.Unmarshal(content, &head)
	return head.Kind
}

// ParseGroup reads and validates one Group manifest.
func ParseGroup(file string, content []byte) (*Group, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	var g Group
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", file, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: only one manifest per file is supported", file)
	}
	g.File = file
	sum := sha256.Sum256(content)
	g.Hash = "sha256:" + hex.EncodeToString(sum[:])

	switch {
	case g.APIVersion != APIVersion:
		return nil, fmt.Errorf("%s: apiVersion must be %q", file, APIVersion)
	case g.Kind != KindGroup:
		return nil, fmt.Errorf("%s: kind must be %q", file, KindGroup)
	case !nameRe.MatchString(g.Metadata.Name):
		return nil, fmt.Errorf("%s: metadata.name must be lowercase letters, digits and dashes (max 63)", file)
	case strings.TrimSpace(g.Spec.CloudAccount) == "":
		return nil, fmt.Errorf("%s: spec.cloudAccount is required (the name of a cloud account attached to the repository)", file)
	}
	seen := map[string]bool{}
	for _, a := range g.Spec.After {
		switch {
		case !nameRe.MatchString(a):
			return nil, fmt.Errorf("%s: spec.after: invalid group name %q", file, a)
		case a == g.Metadata.Name:
			return nil, fmt.Errorf("%s: spec.after: a group can't come after itself", file)
		case seen[a]:
			return nil, fmt.Errorf("%s: spec.after: %q is listed twice", file, a)
		}
		seen[a] = true
	}
	return &g, nil
}

// Parse reads and validates one manifest file.
func Parse(file string, content []byte) (*Application, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)

	var app Application
	if err := dec.Decode(&app); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", file, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: only one Application per file is supported", file)
	}

	app.File = file
	sum := sha256.Sum256(content)
	app.Hash = "sha256:" + hex.EncodeToString(sum[:])

	if err := app.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &app, nil
}

func (a *Application) validate() error {
	if a.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if a.Kind != Kind {
		return fmt.Errorf("kind must be %q", Kind)
	}
	if !nameRe.MatchString(a.Metadata.Name) {
		return errors.New("metadata.name must be lowercase letters, digits and dashes (max 63)")
	}

	switch a.Spec.Lifecycle {
	case "", LifecycleJob:
	default:
		return fmt.Errorf(`spec.lifecycle must be %q (or omitted)`, LifecycleJob)
	}
	if a.Spec.Group != "" && len(a.Spec.Groups) > 0 {
		return errors.New("spec: use either group or groups, not both")
	}
	if a.Spec.Group != "" {
		a.Spec.Groups = []string{a.Spec.Group}
		a.Spec.Group = ""
	}
	seenGroups := map[string]bool{}
	for _, g := range a.Spec.Groups {
		switch {
		case !nameRe.MatchString(g):
			return fmt.Errorf("spec.groups: invalid group name %q", g)
		case seenGroups[g]:
			return fmt.Errorf("spec.groups: %q is listed twice", g)
		}
		seenGroups[g] = true
	}
	if len(a.Spec.Groups) > 0 && a.Spec.Destination.CloudAccount != "" {
		return errors.New("spec.destination.cloudAccount can't be used with groups: the Group manifest sets the cloud account")
	}

	if r := strings.TrimSpace(a.Spec.Source.Repo); r != "" {
		if !repoRe.MatchString(r) {
			return fmt.Errorf("spec.source.repo must be a GitHub repository \"owner/name\", got %q", r)
		}
		a.Spec.Source.Repo = r
	}
	p := strings.TrimSpace(a.Spec.Source.Path)
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return errors.New("spec.source.path must be a relative directory inside the repository")
	}
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return errors.New("spec.source.path must be a directory inside the repository")
	}
	a.Spec.Source.Path = p

	seenDeps := map[string]bool{}
	for _, d := range a.Spec.SyncPolicy.DependsOn {
		switch {
		case !validDep(d):
			return fmt.Errorf("spec.syncPolicy.dependsOn: invalid Application %q (use name, or name@group for another group)", d)
		case d == a.Metadata.Name:
			return errors.New("spec.syncPolicy.dependsOn: an Application can't depend on itself")
		case seenDeps[d]:
			return fmt.Errorf("spec.syncPolicy.dependsOn: %q is listed twice", d)
		}
		seenDeps[d] = true
	}

	switch a.Spec.SyncPolicy.Apply {
	case "", "manual", "auto":
	default:
		return errors.New(`spec.syncPolicy.apply must be "auto" or "manual"`)
	}

	if a.Spec.EditScope.All && len(a.Spec.EditScope.Rules) > 0 {
		return errors.New("spec.editScope: use either all: true or rules, not both")
	}
	for i, r := range a.Spec.EditScope.Rules {
		if len(r.Files) == 0 {
			return fmt.Errorf("spec.editScope.rules[%d].files is required", i)
		}
		if len(r.Blocks) == 0 {
			return fmt.Errorf("spec.editScope.rules[%d].blocks is required (use [\"*\"] for any block)", i)
		}
		for _, f := range r.Files {
			if f == "" || strings.HasPrefix(f, "/") || strings.Contains(f, "..") {
				return fmt.Errorf("spec.editScope.rules[%d].files: %q must be a relative glob without ..", i, f)
			}
		}
		for _, b := range r.Blocks {
			if !blockPatRe.MatchString(b) {
				return fmt.Errorf("spec.editScope.rules[%d].blocks: invalid block pattern %q", i, b)
			}
		}
	}
	switch a.Spec.Source.Engine {
	case "", EngineOpenTofu, EngineTerraform:
	default:
		return fmt.Errorf(`spec.source.engine must be "opentofu" or "terraform"`)
	}
	for i, r := range a.Spec.SyncPolicy.Rules {
		switch r.Apply {
		case ApplyAuto, ApplyManual, ApplyReject:
		default:
			return fmt.Errorf(`spec.syncPolicy.rules[%d].apply must be "auto", "manual" or "reject"`, i)
		}
		for _, act := range r.When.Actions {
			switch act {
			case ActionCreate, ActionUpdate, ActionDelete, ActionReplace:
			default:
				return fmt.Errorf("spec.syncPolicy.rules[%d].when.actions: unknown action %q", i, act)
			}
		}
		for _, res := range r.When.Resources {
			if !blockPatRe.MatchString(res) {
				return fmt.Errorf("spec.syncPolicy.rules[%d].when.resources: invalid pattern %q", i, res)
			}
		}
	}

	for name, v := range a.Spec.Variables {
		if !varNameRe.MatchString(name) {
			return fmt.Errorf("spec.variables: invalid variable name %q", name)
		}
		if _, err := VariableValue(v); err != nil {
			return fmt.Errorf("spec.variables.%s: %w", name, err)
		}
	}
	return nil
}

// validDep accepts "app" (same group) or "app@group" (an Application of
// another group).
func validDep(d string) bool {
	app, group, explicit := strings.Cut(d, "@")
	return nameRe.MatchString(app) && (!explicit || nameRe.MatchString(group))
}

// VariableValue renders a manifest variable the way tofu reads TF_VAR_*:
// strings as-is, everything else (numbers, bools, lists, maps) as JSON.
func VariableValue(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", errors.New("null values are not allowed")
	case string:
		return t, nil
	}
	b, err := json.Marshal(normalizeYAML(v))
	if err != nil {
		return "", fmt.Errorf("unsupported value: %w", err)
	}
	return string(b), nil
}

// normalizeYAML converts map[any]any (possible in YAML) to map[string]any so
// values can be JSON-encoded.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeYAML(val)
		}
		return out
	}
	return v
}

// LoadResult is every Application instance and Group found in a
// repository, plus the manifests that could not be loaded.
type LoadResult struct {
	Apps     []*Application
	Groups   []*Group
	Diagrams []*Diagram
	Jobs     []*Job
	Errors   []error
	// Warnings don't invalidate anything (e.g. a Diagram naming an
	// Application that isn't in its group).
	Warnings []error
}

// ByName finds an Application instance ("app" or "app@group").
func (r LoadResult) ByName(name string) *Application {
	for _, a := range r.Apps {
		if a.Metadata.Name == name {
			return a
		}
	}
	return nil
}

// ByFile finds the (first) Application instance declared in a manifest
// file.
func (r LoadResult) ByFile(file string) *Application {
	for _, a := range r.Apps {
		if a.File == file {
			return a
		}
	}
	return nil
}

// Group finds a Group by name.
func (r LoadResult) Group(name string) *Group {
	for _, g := range r.Groups {
		if g.Metadata.Name == name {
			return g
		}
	}
	return nil
}

// Load parses every manifest of the default layout (.towercrane/); see
// LoadIn.
func Load(files map[string]string) LoadResult { return LoadIn(files, DefaultLayout()) }

// LoadIn parses every manifest in files (repository path -> content) where
// the layout keeps them, and expands Applications into one instance per
// group. Invalid manifests, duplicate names, unknown groups and broken
// dependencies are reported in Errors and left out. When the whole
// repository holds configuration, YAML files without a TowerCrane apiVersion
// (CI workflows and such) are not manifests and are skipped silently.
func LoadIn(files map[string]string, layout Layout) LoadResult {
	var paths []string
	for p := range files {
		if !layout.Holds(p) {
			continue
		}
		if layout.Root() && !isTowerCraneYAML([]byte(files[p])) {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var res LoadResult
	var apps []*Application
	seen := map[string]string{}
	seenGroups := map[string]string{}
	seenDiagrams := map[string]string{}
	seenJobs := map[string]string{}
	for _, p := range paths {
		if kindOf([]byte(files[p])) == KindProject {
			continue // the project itself (FindProject), not one of its manifests
		}
		if kindOf([]byte(files[p])) == KindJob {
			j, err := ParseJob(p, []byte(files[p]))
			if err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			if other, dup := seenJobs[j.Metadata.Name]; dup {
				res.Errors = append(res.Errors, fmt.Errorf("%s: Job %q is already defined in %s", p, j.Metadata.Name, other))
				continue
			}
			seenJobs[j.Metadata.Name] = p
			res.Jobs = append(res.Jobs, j)
			continue
		}
		if kindOf([]byte(files[p])) == KindDiagram {
			d, err := ParseDiagram(p, []byte(files[p]))
			if err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			if other, dup := seenDiagrams[d.Spec.Group]; dup {
				res.Errors = append(res.Errors, fmt.Errorf("%s: group %q already has a Diagram in %s", p, d.Spec.Group, other))
				continue
			}
			seenDiagrams[d.Spec.Group] = p
			res.Diagrams = append(res.Diagrams, d)
			continue
		}
		if kindOf([]byte(files[p])) == KindGroup {
			g, err := ParseGroup(p, []byte(files[p]))
			if err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			if other, dup := seenGroups[g.Metadata.Name]; dup {
				res.Errors = append(res.Errors, fmt.Errorf("%s: Group %q is already defined in %s", p, g.Metadata.Name, other))
				continue
			}
			seenGroups[g.Metadata.Name] = p
			res.Groups = append(res.Groups, g)
			continue
		}
		app, err := Parse(p, []byte(files[p]))
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		// The same name may be used in different groups (sandbox/ecr and
		// prod/ecr are different directories): instances must be unique.
		instances := []string{app.Metadata.Name}
		if len(app.Spec.Groups) > 0 {
			instances = nil
			for _, g := range app.Spec.Groups {
				instances = append(instances, InstanceName(app.Metadata.Name, g))
			}
		}
		dup := ""
		for _, inst := range instances {
			if other, ok := seen[inst]; ok {
				dup = fmt.Sprintf("%s: Application %q is already defined in %s", p, inst, other)
				break
			}
		}
		if dup != "" {
			res.Errors = append(res.Errors, errors.New(dup))
			continue
		}
		for _, inst := range instances {
			seen[inst] = p
		}
		apps = append(apps, app)
	}
	res.checkGroups()
	res.expand(apps)
	res.checkDependencies()
	res.addPromotion()
	res.checkDiagrams()
	res.checkJobs()
	return res
}

// checkJobs drops Jobs whose Application doesn't exist or isn't ephemeral
// (lifecycle: job), and Applications that depend on an ephemeral one (it only
// exists while its Job keeps it up, so a rollout would wait forever).
func (r *LoadResult) checkJobs() {
	ephemeral := map[string]bool{}
	for _, a := range r.Apps {
		if a.Spec.Lifecycle == LifecycleJob {
			ephemeral[a.Metadata.Name] = true
		}
	}
	var keptApps []*Application
	for _, a := range r.Apps {
		bad := ""
		if !ephemeral[a.Metadata.Name] {
			for _, d := range a.Deps() {
				if ephemeral[d] {
					bad = d
					break
				}
			}
		}
		if bad != "" {
			r.Errors = append(r.Errors, fmt.Errorf("%s: %s depends on %s, which only exists while its Job keeps it up (lifecycle: job)",
				a.File, a.Metadata.Name, bad))
			continue
		}
		keptApps = append(keptApps, a)
	}
	r.Apps = keptApps

	owner := map[string]string{}
	var kept []*Job
	for _, j := range r.Jobs {
		inst := j.Instance()
		app := r.ByName(inst)
		switch {
		case app == nil:
			r.Errors = append(r.Errors, fmt.Errorf("%s: Application %q not found (spec.application / spec.group)", j.File, inst))
		case app.Spec.Lifecycle != LifecycleJob:
			r.Errors = append(r.Errors, fmt.Errorf("%s: %s must declare spec.lifecycle: job in %s to be run by a Job (so merges don't apply it and only its Job destroys it)",
				j.File, inst, app.File))
		case owner[inst] != "":
			r.Errors = append(r.Errors, fmt.Errorf("%s: %s is already run by Job %q", j.File, inst, owner[inst]))
		default:
			owner[inst] = j.Metadata.Name
			kept = append(kept, j)
		}
	}
	r.Jobs = kept
}

// JobFor returns the Job that runs an Application instance, or nil.
func (r LoadResult) JobFor(instance string) *Job {
	for _, j := range r.Jobs {
		if j.Instance() == instance {
			return j
		}
	}
	return nil
}

// JobByName finds a Job.
func (r LoadResult) JobByName(name string) *Job {
	for _, j := range r.Jobs {
		if j.Metadata.Name == name {
			return j
		}
	}
	return nil
}

// checkDiagrams warns about Diagram references to unknown groups or to
// Applications that aren't in the group. The Diagram is kept (it is only
// visual); the UI ignores what doesn't exist.
func (r *LoadResult) checkDiagrams() {
	for _, d := range r.Diagrams {
		if d.Spec.Group != "" && r.Group(d.Spec.Group) == nil {
			r.Warnings = append(r.Warnings, fmt.Errorf("%s: spec.group %q: no valid Group with that name", d.File, d.Spec.Group))
			continue
		}
		member := map[string]bool{}
		for _, a := range r.Apps {
			if a.Group == d.Spec.Group {
				member[a.App] = true
			}
		}
		var unknown []string
		check := func(name string) {
			if !member[name] && !contains(unknown, name) {
				unknown = append(unknown, name)
			}
		}
		for _, s := range d.Spec.Sections {
			for _, a := range s.Apps {
				check(a)
			}
		}
		for name := range d.Spec.Nodes {
			check(name)
		}
		for _, l := range d.Spec.Links {
			check(l.From)
			check(l.To)
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			r.Warnings = append(r.Warnings, fmt.Errorf("%s: not Applications of group %q (ignored in the diagram): %s",
				d.File, d.Spec.Group, strings.Join(unknown, ", ")))
		}
	}
}

// Summary is a short description of a Diagram for approval requests.
func (d *Diagram) Summary() []string {
	return []string{
		"diagram of " + firstNonEmpty(d.Spec.Group, "Applications without group"),
		fmt.Sprintf("sections=%d", len(d.Spec.Sections)),
		fmt.Sprintf("styled nodes=%d", len(d.Spec.Nodes)),
		fmt.Sprintf("links=%d", len(d.Spec.Links)),
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// DiagramFor returns the Diagram of a group ("" = Applications without
// group), or nil.
func (r LoadResult) DiagramFor(group string) *Diagram {
	for _, d := range r.Diagrams {
		if d.Spec.Group == group {
			return d
		}
	}
	return nil
}

// checkGroups drops Groups whose spec.after names a missing Group or forms
// a cycle (transitively).
func (r *LoadResult) checkGroups() {
	for {
		byName := map[string]*Group{}
		for _, g := range r.Groups {
			byName[g.Metadata.Name] = g
		}
		bad := map[string]error{}
		for _, g := range r.Groups {
			for _, a := range g.Spec.After {
				if byName[a] == nil {
					bad[g.Metadata.Name] = fmt.Errorf("%s: spec.after %q: no valid Group with that name in %s/", g.File, a, Dir)
					break
				}
			}
		}
		if len(bad) == 0 {
			deps := map[string][]string{}
			for _, g := range r.Groups {
				deps[g.Metadata.Name] = g.Spec.After
			}
			if cycle := cycleIn(deps); len(cycle) > 0 {
				msg := strings.Join(cycle, " -> ")
				for _, name := range cycle[:len(cycle)-1] {
					bad[name] = fmt.Errorf("%s: spec.after cycle: %s", byName[name].File, msg)
				}
			}
		}
		if len(bad) == 0 {
			return
		}
		var kept []*Group
		for _, g := range r.Groups {
			if err, ok := bad[g.Metadata.Name]; ok {
				r.Errors = append(r.Errors, err)
				continue
			}
			kept = append(kept, g)
		}
		r.Groups = kept
	}
}

// expand turns Applications into instances: one per group, or the
// Application itself when it has no groups.
func (r *LoadResult) expand(apps []*Application) {
	grouped := map[string]map[string]bool{} // app name -> its groups (across manifests)
	for _, a := range apps {
		if grouped[a.Metadata.Name] == nil {
			grouped[a.Metadata.Name] = map[string]bool{}
		}
		for _, g := range a.Spec.Groups {
			grouped[a.Metadata.Name][g] = true
		}
	}
	for _, a := range apps {
		name := a.Metadata.Name
		if len(a.Spec.Groups) == 0 {
			a.App = name
			a.CloudAccount = a.Spec.Destination.CloudAccount
			for _, d := range a.Spec.SyncPolicy.DependsOn {
				if !strings.Contains(d, "@") && len(grouped[d]) > 0 {
					r.Errors = append(r.Errors, fmt.Errorf("%s: dependsOn %q: it is in groups; name the instance, e.g. %s", a.File, d, InstanceName(d, firstKey(grouped[d]))))
					a = nil
					break
				}
			}
			if a != nil {
				r.Apps = append(r.Apps, a)
			}
			continue
		}
		for _, gname := range a.Spec.Groups {
			g := r.Group(gname)
			if g == nil {
				r.Errors = append(r.Errors, fmt.Errorf("%s: group %q: no valid Group with that name in %s/ (%s left out)",
					a.File, gname, Dir, InstanceName(name, gname)))
				continue
			}
			inst := *a
			inst.Metadata.Name = InstanceName(name, gname)
			inst.App = name
			inst.Group = gname
			inst.GroupFile = g.File
			inst.CloudAccount = g.Spec.CloudAccount
			sum := sha256.Sum256([]byte(a.Hash + "\n" + g.Hash))
			inst.Hash = "sha256:" + hex.EncodeToString(sum[:])
			// dependsOn inside the same group; an Application without
			// groups is shared by every group.
			deps := make([]string, 0, len(a.Spec.SyncPolicy.DependsOn))
			for _, d := range a.Spec.SyncPolicy.DependsOn {
				switch {
				case strings.Contains(d, "@"):
					deps = append(deps, d) // explicit instance of another group
				case len(grouped[d]) == 0 && grouped[d] != nil:
					deps = append(deps, d) // an Application without groups
				default:
					deps = append(deps, InstanceName(d, gname)) // same group
				}
			}
			inst.Spec.SyncPolicy.DependsOn = deps
			r.Apps = append(r.Apps, &inst)
		}
	}
}

func firstKey(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// addPromotion makes every instance of a group wait for all instances of
// the groups it comes after (Group spec.after).
func (r *LoadResult) addPromotion() {
	byGroup := map[string][]string{}
	for _, a := range r.Apps {
		if a.Group != "" {
			byGroup[a.Group] = append(byGroup[a.Group], a.Metadata.Name)
		}
	}
	for _, a := range r.Apps {
		if a.Group == "" {
			continue
		}
		for _, before := range r.Group(a.Group).Spec.After {
			a.Promotion = append(a.Promotion, byGroup[before]...)
		}
		sort.Strings(a.Promotion)
	}
}

// checkDependencies drops Applications whose dependsOn references a missing
// (or invalid) Application or is part of a cycle, reporting why. Dropping is
// transitive: whatever depends on a dropped Application is dropped too, so
// the remaining set always has a valid apply order.
func (r *LoadResult) checkDependencies() {
	for {
		byName := map[string]*Application{}
		for _, a := range r.Apps {
			byName[a.Metadata.Name] = a
		}
		bad := map[string]error{}
		for _, a := range r.Apps {
			for _, d := range a.Spec.SyncPolicy.DependsOn {
				if byName[d] == nil {
					bad[a.Metadata.Name] = fmt.Errorf("%s: dependsOn %q: no valid Application with that name in %s/", a.File, d, Dir)
					break
				}
			}
		}
		if len(bad) == 0 {
			if cycle := findCycle(r.Apps); len(cycle) > 0 {
				msg := strings.Join(cycle, " -> ")
				for _, name := range cycle[:len(cycle)-1] {
					bad[name] = fmt.Errorf("%s: dependsOn cycle: %s", byName[name].File, msg)
				}
			}
		}
		if len(bad) == 0 {
			return
		}
		var kept []*Application
		for _, a := range r.Apps {
			if err, ok := bad[a.Metadata.Name]; ok {
				r.Errors = append(r.Errors, err)
				continue
			}
			kept = append(kept, a)
		}
		r.Apps = kept
	}
}

// findCycle returns one dependency cycle as a path (first == last), or nil.
func findCycle(apps []*Application) []string {
	deps := map[string][]string{}
	for _, a := range apps {
		deps[a.Metadata.Name] = a.Deps()
	}
	return cycleIn(deps)
}

// cycleIn returns one cycle of a dependency graph (first == last), or nil.
func cycleIn(deps map[string][]string) []string {
	var names []string
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	var cycle []string
	var visit func(n string) bool
	visit = func(n string) bool {
		color[n] = grey
		stack = append(stack, n)
		for _, d := range deps[n] {
			switch color[d] {
			case grey:
				i := len(stack) - 1
				for stack[i] != d {
					i--
				}
				cycle = append(append([]string{}, stack[i:]...), d)
				return true
			case white:
				if visit(d) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for _, n := range names {
		if color[n] == white && visit(n) {
			return cycle
		}
	}
	return nil
}

// ApplyOrder returns Application names so that every Application comes
// after the ones it depends on; ties keep alphabetical order. apps must be a
// LoadResult's Apps (dependencies valid, no cycles).
func ApplyOrder(apps []*Application) []string {
	byName := map[string]*Application{}
	var pending []string
	for _, a := range apps {
		byName[a.Metadata.Name] = a
		pending = append(pending, a.Metadata.Name)
	}
	sort.Strings(pending)
	done := map[string]bool{}
	var out []string
	for len(pending) > 0 {
		progressed := false
		for i, n := range pending {
			ready := true
			for _, d := range byName[n].Deps() {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				out = append(out, n)
				done[n] = true
				pending = append(pending[:i], pending[i+1:]...)
				progressed = true
				break
			}
		}
		if !progressed { // cannot happen after checkDependencies
			out = append(out, pending...)
			break
		}
	}
	return out
}

// ValidOrder reports whether order lists exactly these Applications with
// every one after its dependencies.
func ValidOrder(apps []*Application, order []string) error {
	if len(order) != len(apps) {
		return fmt.Errorf("the order must list all %d Applications", len(apps))
	}
	byName := map[string]*Application{}
	for _, a := range apps {
		byName[a.Metadata.Name] = a
	}
	pos := map[string]int{}
	for i, n := range order {
		if byName[n] == nil {
			return fmt.Errorf("unknown Application %q", n)
		}
		if _, dup := pos[n]; dup {
			return fmt.Errorf("%q is listed twice", n)
		}
		pos[n] = i
	}
	for _, n := range order {
		for _, d := range byName[n].Deps() {
			if pos[d] > pos[n] {
				return fmt.Errorf("%s must come after %s (dependsOn or group order in its manifests)", n, d)
			}
		}
	}
	return nil
}

// MatchGlob matches a repository path against a glob where "*" matches
// within one path segment and "**" matches any number of segments.
func MatchGlob(pattern, p string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegments(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// MatchBlock matches a block address ("variable.instance_count") against a
// pattern where "*" matches any run of characters, dots included.
func MatchBlock(pattern, address string) bool {
	re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	ok, _ := regexp.MatchString(re, address)
	return ok
}
