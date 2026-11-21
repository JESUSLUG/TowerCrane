package tfmap

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
)

// ViolationKind classifies why a change is outside an Application's
// edit scope.
type ViolationKind string

const (
	// The changed file matches no editScope rule.
	ViolationFileNotInScope ViolationKind = "file-not-in-scope"
	// A changed block isn't allowed by the rules matching its file.
	ViolationBlockNotInScope ViolationKind = "block-not-in-scope"
	// A non-HCL file (or one TowerCrane can't parse) changed and no rule
	// allows the whole file (blocks: ["*"]).
	ViolationWholeFile ViolationKind = "whole-file-not-in-scope"
	// The plan changes a resource the edit scope doesn't cover.
	ViolationPlanOutOfScope ViolationKind = "plan-out-of-scope"
)

type Violation struct {
	Kind    ViolationKind `json:"kind"`
	File    string        `json:"file,omitempty"`
	Block   string        `json:"block,omitempty"`
	Address string        `json:"address,omitempty"`
	Action  string        `json:"action,omitempty"`
	Message string        `json:"message"`
}

func (v Violation) String() string {
	target := v.File
	if v.Block != "" {
		target += " " + v.Block
	}
	if v.Address != "" {
		target = v.Address
		if v.Action != "" {
			target += " (" + v.Action + ")"
		}
	}
	return fmt.Sprintf("%s: %s", target, v.Message)
}

// underAny reports whether file lives in, or below, one of dirs.
func underAny(file string, dirs map[string]bool) bool {
	for d := path.Dir(file); ; d = path.Dir(d) {
		if dirs[d] {
			return true
		}
		if d == "." || d == "/" {
			return false
		}
	}
}

// isIgnoredFile lists files that can never affect what tofu does.
func isIgnoredFile(p string) bool {
	base := strings.ToLower(path.Base(p))
	return strings.HasSuffix(base, ".md") || base == "license" || base == ".gitignore" || manifest.IsManifestPath(p)
}

// AppFiles returns which of changed belong to an Application: files inside
// its module tree in either version (the new tree is what gets applied, the
// old tree is what a removed module call would destroy).
func AppFiles(app *manifest.Application, oldFiles, newFiles map[string]string, changed []string) []string {
	dirs := ReachableDirs(newFiles, app.Spec.Source.Path)
	for d := range ReachableDirs(oldFiles, app.Spec.Source.Path) {
		dirs[d] = true
	}
	extra := ExtraFiles(newFiles, app.Spec.Source.Path)
	for f := range ExtraFiles(oldFiles, app.Spec.Source.Path) {
		extra[f] = true
	}
	var out []string
	for _, p := range changed {
		if (underAny(p, dirs) || extra[p]) && !isIgnoredFile(p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// DiffGate checks the files and blocks a commit changed against the
// Application's edit scope. oldFiles/newFiles hold the .tf content of both
// versions; changed lists every changed path in the repository.
func DiffGate(app *manifest.Application, oldFiles, newFiles map[string]string, changed []string) []Violation {
	scope := app.Spec.EditScope
	var out []Violation
	for _, p := range AppFiles(app, oldFiles, newFiles, changed) {
		if scope.All {
			continue
		}
		var rules []manifest.EditRule
		for _, r := range scope.Rules {
			for _, g := range r.Files {
				if manifest.MatchGlob(g, p) {
					rules = append(rules, r)
					break
				}
			}
		}
		if len(rules) == 0 {
			out = append(out, Violation{Kind: ViolationFileNotInScope, File: p,
				Message: "this file is not in the Application's editScope"})
			continue
		}
		wholeFile := false
		for _, r := range rules {
			for _, b := range r.Blocks {
				if b == "*" {
					wholeFile = true
				}
			}
		}

		var blocks []string
		var err error
		hcl := strings.HasSuffix(p, ".tf") || strings.HasSuffix(p, ".hcl")
		if hcl {
			blocks, err = ChangedBlocks(p, oldFiles[p], newFiles[p])
		}
		if !hcl || err != nil {
			if !wholeFile {
				msg := "not a Terraform file: only a rule with blocks [\"*\"] can allow changing it"
				if err != nil {
					msg = "TowerCrane can't parse this file (" + err.Error() + "): only a rule with blocks [\"*\"] can allow it"
				}
				out = append(out, Violation{Kind: ViolationWholeFile, File: p, Message: msg})
			}
			continue
		}

		for _, id := range blocks {
			allowed := false
			for _, r := range rules {
				for _, b := range r.Blocks {
					if manifest.MatchBlock(b, id) {
						allowed = true
					}
				}
			}
			if !allowed {
				out = append(out, Violation{Kind: ViolationBlockNotInScope, File: p, Block: id,
					Message: "this block is not allowed by the editScope rules for its file"})
			}
		}
	}
	return out
}

// AllowedAddresses is the set of plan addresses an Application may change.
type AllowedAddresses struct {
	all   bool
	exact map[string]bool
	trees []string // allowed address prefixes (module instances)
}

// Matches reports whether a plan address is covered.
func (a AllowedAddresses) Matches(address string) bool {
	if a.all {
		return true
	}
	norm := NormalizeAddress(address)
	if a.exact[norm] {
		return true
	}
	for _, t := range a.trees {
		if strings.HasPrefix(norm, t) {
			return true
		}
	}
	return false
}

// Describe lists the allowed addresses (for messages and the UI).
func (a AllowedAddresses) Describe() []string {
	if a.all {
		return []string{"*"}
	}
	var out []string
	for e := range a.exact {
		out = append(out, e)
	}
	for _, t := range a.trees {
		out = append(out, t+"*")
	}
	sort.Strings(out)
	return out
}

// NormalizeAddress strips instance keys from a plan address:
// module.a["x"].aws_instance.main[0] -> module.a.aws_instance.main.
func NormalizeAddress(address string) string { return manifest.StripIndexes(address) }

// Allowed computes the plan addresses an Application's rules allow, given
// the repository's .tf files at the commit being applied:
//
//   - an allowed resource/data block allows that resource in every instance
//     of its module;
//   - an allowed module call allows everything inside that module instance;
//   - with dependents: true, whatever uses an allowed block (directly or
//     through locals, variables, outputs and module calls) is allowed too.
func Allowed(app *manifest.Application, files map[string]string) AllowedAddresses {
	if app.Spec.EditScope.All {
		return AllowedAddresses{all: true}
	}
	m := buildModel(files, app.Spec.Source.Path)

	allowed := map[symKey]bool{}
	var queue []symKey
	for _, r := range app.Spec.EditScope.Rules {
		for key, info := range m.symbols {
			if !matchesAny(r.Files, info.file, manifest.MatchGlob) || !matchesAny(r.Blocks, key.id, manifest.MatchBlock) {
				continue
			}
			allowed[key] = true
			if r.Dependents {
				queue = append(queue, key)
			}
		}
	}

	// Reverse references: who uses each symbol (same directory).
	users := map[symKey][]symKey{}
	for key, info := range m.symbols {
		for _, ref := range info.refs {
			target := symKey{key.dir, ref}
			users[target] = append(users[target], key)
		}
	}
	visited := map[symKey]bool{}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		if visited[k] {
			continue
		}
		visited[k] = true

		next := append([]symKey{}, users[k]...)
		// A module's output flows to whatever uses the module call.
		if strings.HasPrefix(k.id, "output.") {
			for dir, calls := range m.calls {
				for name, target := range calls {
					if target == k.dir {
						next = append(next, users[symKey{dir, "module." + name}]...)
					}
				}
			}
		}
		for _, u := range next {
			allowed[u] = true
			queue = append(queue, u)
		}
	}

	res := AllowedAddresses{exact: map[string]bool{}}
	for key := range allowed {
		// Terragrunt unit blocks (inputs, dependency...) feed the whole
		// module: with dependents, anything in it may change.
		if m.unitDir != "" && key.dir == m.unitDir && visited[key] {
			return AllowedAddresses{all: true}
		}
		for _, prefix := range m.prefixes[key.dir] {
			switch {
			case strings.HasPrefix(key.id, "resource."):
				res.exact[prefix+strings.TrimPrefix(key.id, "resource.")] = true
			case strings.HasPrefix(key.id, "data."):
				res.exact[prefix+key.id] = true
			case strings.HasPrefix(key.id, "module."):
				res.trees = append(res.trees, prefix+key.id+".")
			}
		}
	}
	sort.Strings(res.trees)
	return res
}

func matchesAny(patterns []string, s string, match func(string, string) bool) bool {
	for _, p := range patterns {
		if match(p, s) {
			return true
		}
	}
	return false
}

// PlanChange is one resource change from `tofu show -json` (resource_changes).
type PlanChange struct {
	Address string
	// Action is create, update, delete or replace ("" for no-op/read).
	Action string
}

// PlanActionFromTofu maps tofu's actions list to a single action.
func PlanActionFromTofu(actions []string) string {
	has := map[string]bool{}
	for _, a := range actions {
		has[a] = true
	}
	switch {
	case has["delete"] && has["create"]:
		return manifest.ActionReplace
	case has["create"]:
		return manifest.ActionCreate
	case has["update"]:
		return manifest.ActionUpdate
	case has["delete"]:
		return manifest.ActionDelete
	}
	return "" // no-op, read
}

// PlanGate checks the real plan against the Application's editScope:
// every changed resource must be covered. (How the plan is applied is
// decided separately by the syncPolicy rules, see manifest.Decide.)
func PlanGate(app *manifest.Application, files map[string]string, changes []PlanChange) []Violation {
	allowed := Allowed(app, files)
	var out []Violation
	for _, c := range changes {
		if c.Action == "" {
			continue
		}
		if !allowed.Matches(c.Address) {
			out = append(out, Violation{Kind: ViolationPlanOutOfScope, Address: c.Address, Action: c.Action,
				Message: "the plan changes a resource outside the Application's editScope"})
		}
	}
	return out
}

// NodeEditable reports whether an Application's rules allow changing a
// map node's block.
func NodeEditable(app *manifest.Application, n Node) bool {
	if app.Spec.EditScope.All {
		return true
	}
	for _, r := range app.Spec.EditScope.Rules {
		if matchesAny(r.Files, n.FilePath, manifest.MatchGlob) && matchesAny(r.Blocks, n.BlockAddress(), manifest.MatchBlock) {
			return true
		}
	}
	return false
}
