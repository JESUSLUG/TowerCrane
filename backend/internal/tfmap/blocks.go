package tfmap

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// Block addresses used by Application manifests (spec.editScope.rules.blocks):
//
//	resource.<type>.<name>   data.<type>.<name>   module.<name>
//	variable.<name>          output.<name>        local.<name>
//	provider.<name>          terraform            moved / removed / import
//	check.<name>
//
// Terragrunt files (terragrunt.hcl, root.hcl) use the same scheme:
// dependency.<name>, include.<name>, generate.<name>, remote_state,
// terraform, local.<name>, and top-level attributes by name (inputs).
//
// A `locals { }` block is split into one local.<name> per attribute, so a
// manifest can allow changing one local without allowing all of them.

// fileSymbol is one addressable piece of a .tf file.
type fileSymbol struct {
	id   string
	text string          // exact source, used to detect changes
	refs []hcl.Traversal // what it references
	body *hclsyntax.Body // module calls: to read `source`
	expr hclsyntax.Expression
}

// parseSymbols splits a .tf file into addressable symbols.
func parseSymbols(filename, content string) ([]fileSymbol, error) {
	f, diags := hclsyntax.ParseConfig([]byte(content), filename, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s: %s", filename, diags.Error())
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected HCL body", filename)
	}
	src := []byte(content)
	text := func(r hcl.Range) string { return string(r.SliceBytes(src)) }

	var out []fileSymbol
	for name, attr := range body.Attributes {
		// Top-level attributes are not valid Terraform; still track them so
		// a change is never invisible.
		// Terragrunt's `inputs = {...}`; not valid in plain Terraform, still
		// tracked so a change is never invisible.
		out = append(out, fileSymbol{id: name, text: text(attr.SrcRange), refs: attr.Expr.Variables()})
	}
	for _, b := range body.Blocks {
		label := func(i int) string {
			if i < len(b.Labels) {
				return b.Labels[i]
			}
			return "_"
		}
		var id string
		switch b.Type {
		case "resource":
			id = "resource." + label(0) + "." + label(1)
		case "data":
			id = "data." + label(0) + "." + label(1)
		case "module", "variable", "output", "provider", "check",
			"dependency", "include", "generate": // the last three: Terragrunt
			id = b.Type + "." + label(0)
		case "locals":
			for name, attr := range b.Body.Attributes {
				out = append(out, fileSymbol{id: "local." + name, text: text(attr.SrcRange), refs: attr.Expr.Variables()})
			}
			continue
		default: // terraform, moved, removed, import, ...
			id = b.Type
		}
		out = append(out, fileSymbol{id: id, text: text(b.Range()), refs: bodyRefs(b.Body), body: b.Body})
	}
	return out, nil
}

// bodyRefs collects every traversal referenced anywhere in a block body.
func bodyRefs(body *hclsyntax.Body) []hcl.Traversal {
	var out []hcl.Traversal
	for _, attr := range body.Attributes {
		out = append(out, attr.Expr.Variables()...)
	}
	for _, nested := range body.Blocks {
		out = append(out, bodyRefs(nested.Body)...)
	}
	return out
}

// ChangedBlocks returns the block addresses that differ between two versions
// of a .tf file (added, removed or modified). Comments and whitespace
// outside blocks are ignored. An empty version means the file didn't exist.
func ChangedBlocks(filename, oldContent, newContent string) ([]string, error) {
	collect := func(content string) (map[string][]string, error) {
		m := map[string][]string{}
		if strings.TrimSpace(content) == "" {
			return m, nil
		}
		syms, err := parseSymbols(filename, content)
		if err != nil {
			return nil, err
		}
		for _, s := range syms {
			m[s.id] = append(m[s.id], s.text)
		}
		for id := range m {
			sort.Strings(m[id])
		}
		return m, nil
	}
	before, err := collect(oldContent)
	if err != nil {
		return nil, err
	}
	after, err := collect(newContent)
	if err != nil {
		return nil, err
	}

	ids := map[string]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	var changed []string
	for id := range ids {
		if strings.Join(before[id], "\x00") != strings.Join(after[id], "\x00") {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

// refSymbol maps a reference to the block address it points at, within the
// same module directory.
func refSymbol(t hcl.Traversal) (string, bool) {
	if len(t) == 0 {
		return "", false
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return "", false
	}
	attr := func(i int) (string, bool) {
		if i >= len(t) {
			return "", false
		}
		a, ok := t[i].(hcl.TraverseAttr)
		if !ok {
			return "", false
		}
		return a.Name, true
	}
	switch root.Name {
	case "var":
		if n, ok := attr(1); ok {
			return "variable." + n, true
		}
	case "local":
		if n, ok := attr(1); ok {
			return "local." + n, true
		}
	case "module":
		if n, ok := attr(1); ok {
			return "module." + n, true
		}
	case "data":
		typ, ok1 := attr(1)
		n, ok2 := attr(2)
		if ok1 && ok2 {
			return "data." + typ + "." + n, true
		}
	case "path", "terraform", "each", "count", "self":
	default:
		if n, ok := attr(1); ok {
			return "resource." + root.Name + "." + n, true
		}
	}
	return "", false
}

// symKey identifies a symbol in a module directory.
type symKey struct{ dir, id string }

type symInfo struct {
	file string
	refs []string // symbol ids (same dir) this one uses
}

// moduleModel is the parsed module tree of one Application.
type moduleModel struct {
	// unitDir is the Terragrunt unit directory when rootDir has a
	// terragrunt.hcl ("" otherwise). Its blocks (inputs, dependency...) are
	// symbols too, and its module source becomes the root module.
	unitDir string
	// extraFiles are files outside the module tree that still belong to the
	// Application (e.g. the root.hcl a Terragrunt unit includes).
	extraFiles map[string]bool

	symbols map[symKey]*symInfo
	// calls[dir] = module calls in dir: name -> target directory.
	calls map[string]map[string]string
	// prefixes[dir] = plan address prefixes of every instance of dir
	// ("" for the root, "module.compute." for a called module...).
	prefixes map[string][]string
}

// buildModel parses every .tf file under the module tree rooted at rootDir.
func buildModel(files map[string]string, rootDir string) *moduleModel {
	m := &moduleModel{
		extraFiles: map[string]bool{},
		symbols:    map[symKey]*symInfo{},
		calls:      map[string]map[string]string{},
		prefixes:   map[string][]string{},
	}
	parsedDirs := map[string]bool{}
	parseDir := func(dir string) {
		if parsedDirs[dir] {
			return
		}
		parsedDirs[dir] = true
		for p, content := range files {
			if path.Dir(p) != dir || !strings.HasSuffix(p, ".tf") {
				continue
			}
			syms, err := parseSymbols(p, content)
			if err != nil {
				continue
			}
			for _, s := range syms {
				info := &symInfo{file: p}
				for _, t := range s.refs {
					if id, ok := refSymbol(t); ok && id != s.id {
						info.refs = append(info.refs, id)
					}
				}
				m.symbols[symKey{dir, s.id}] = info
				if strings.HasPrefix(s.id, "module.") && s.body != nil {
					if attr, ok := s.body.Attributes["source"]; ok {
						if v, diags := attr.Expr.Value(nil); !diags.HasErrors() && v.Type().FriendlyName() == "string" {
							if target, ok := resolveModuleDir(dir, v.AsString()); ok {
								if m.calls[dir] == nil {
									m.calls[dir] = map[string]string{}
								}
								m.calls[dir][strings.TrimPrefix(s.id, "module.")] = target
							}
						}
					}
				}
			}
		}
	}

	rootDir = path.Clean(rootDir)
	moduleRoot := rootDir
	if unit, ok := files[path.Join(rootDir, "terragrunt.hcl")]; ok {
		m.unitDir = rootDir
		m.prefixes[rootDir] = nil // part of the Application, but holds no resources
		if syms, err := parseSymbols(path.Join(rootDir, "terragrunt.hcl"), unit); err == nil {
			for _, sym := range syms {
				m.symbols[symKey{rootDir, sym.id}] = &symInfo{file: path.Join(rootDir, "terragrunt.hcl")}
			}
		}
		for _, inc := range terragruntIncludes(files, rootDir, unit) {
			m.extraFiles[inc] = true
		}
		if src, ok := terragruntSource(unit); ok {
			if dir, local := resolveModuleDir(rootDir, src); local {
				moduleRoot = dir
			} else {
				moduleRoot = "" // remote module: nothing to parse
			}
		}
	}

	type item struct{ dir, prefix string }
	var queue []item
	if moduleRoot != "" && moduleRoot != m.unitDir {
		queue = []item{{moduleRoot, ""}}
	} else if m.unitDir == "" {
		queue = []item{{rootDir, ""}}
	}
	for len(queue) > 0 && len(m.prefixes) < 500 {
		it := queue[0]
		queue = queue[1:]
		if len(it.prefix) > 2000 { // defensive: recursive module sources
			continue
		}
		m.prefixes[it.dir] = append(m.prefixes[it.dir], it.prefix)
		parseDir(it.dir)
		for name, target := range m.calls[it.dir] {
			queue = append(queue, item{target, it.prefix + "module." + name + "."})
		}
	}
	return m
}

// ReachableDirs returns rootDir plus every local module directory it calls,
// recursively (for a Terragrunt unit: the unit plus its module source tree).
func ReachableDirs(files map[string]string, rootDir string) map[string]bool {
	m := buildModel(files, rootDir)
	out := map[string]bool{}
	for d := range m.prefixes {
		out[d] = true
	}
	return out
}

// ExtraFiles returns files outside the module tree that belong to an
// Application anyway, such as the root.hcl a Terragrunt unit includes.
func ExtraFiles(files map[string]string, rootDir string) map[string]bool {
	return buildModel(files, rootDir).extraFiles
}

// terragruntSource reads `terraform { source = "..." }` from a
// terragrunt.hcl. Terragrunt's "//" subdirectory syntax
// ("../../modules//vpc") is resolved to a plain path.
func terragruntSource(content string) (string, bool) {
	f, diags := hclsyntax.ParseConfig([]byte(content), "terragrunt.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return "", false
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return "", false
	}
	for _, b := range body.Blocks {
		if b.Type != "terraform" {
			continue
		}
		attr, ok := b.Body.Attributes["source"]
		if !ok {
			return "", false
		}
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || v.Type().FriendlyName() != "string" {
			return "", false
		}
		src := v.AsString()
		if i := strings.Index(src, "//"); i >= 0 && !strings.Contains(src[:i], ":") {
			src = strings.TrimSuffix(src[:i], "/") + "/" + src[i+2:]
		}
		return src, true
	}
	return "", false
}

// terragruntIncludes finds the files a unit includes with
// find_in_parent_folders("name") (the common root.hcl pattern).
func terragruntIncludes(files map[string]string, unitDir, content string) []string {
	var out []string
	for _, m := range findParentRe.FindAllStringSubmatch(content, -1) {
		name := m[1]
		if name == "" {
			name = "terragrunt.hcl"
		}
		for d := path.Dir(unitDir); ; d = path.Dir(d) {
			candidate := path.Join(d, name)
			if _, ok := files[candidate]; ok {
				out = append(out, candidate)
				break
			}
			if d == "." || d == "/" {
				break
			}
		}
	}
	return out
}

var findParentRe = regexp.MustCompile(`find_in_parent_folders\(\s*(?:"([^"]*)")?\s*\)`)

// ReachableFiles returns every file in files that lives in a directory
// reachable from rootDir.
func ReachableFiles(files map[string]string, rootDir string) map[string]bool {
	dirs := ReachableDirs(files, rootDir)
	out := map[string]bool{}
	for p := range files {
		if dirs[path.Dir(p)] {
			out[p] = true
		}
	}
	return out
}
