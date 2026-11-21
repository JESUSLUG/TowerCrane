package tfmap

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// NodeKind identifies what a graph node represents.
type NodeKind string

const (
	KindModule   NodeKind = "module"
	KindResource NodeKind = "resource"
	KindData     NodeKind = "data"
)

// Node is a single element shown on the resource map. Every block in the
// repository becomes a node so the map always shows how things connect;
// which Application a node belongs to, whether it may be edited and whether
// its source is shown are decided by the Application manifests (filled in
// by the caller).
type Node struct {
	ID        string   `json:"id"`
	Address   string   `json:"address"`
	Label     string   `json:"label"`
	Kind      NodeKind `json:"kind"`
	Dir       string   `json:"dir"`
	FilePath  string   `json:"file_path"`
	ParentID  string   `json:"parent_id,omitempty"`
	StartLine int      `json:"start_line,omitempty"`
	EndLine   int      `json:"end_line,omitempty"`

	// Set from the Application manifests.
	Applications []string `json:"applications"`
	Editable     bool     `json:"editable"`
	Source       string   `json:"source,omitempty"`
}

// BlockAddress is the manifest block address of a node
// ("resource.aws_instance.main", "data.aws_ami.ubuntu", "module.compute").
func (n Node) BlockAddress() string {
	if n.Kind == KindResource {
		return "resource." + n.Address
	}
	return n.Address
}

// EdgeKind differentiates structural containment from data dependencies.
type EdgeKind string

const (
	EdgeContains  EdgeKind = "contains"
	EdgeDependsOn EdgeKind = "depends-on"
)

type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Kind EdgeKind `json:"kind"`
}

// Graph is the full map: nodes TowerCrane may show, plus the edges between
// them, and the list of directories that look like environment roots (i.e.
// are never used as another module's source).
type Graph struct {
	Nodes    []Node   `json:"nodes"`
	Edges    []Edge   `json:"edges"`
	RootDirs []string `json:"root_dirs"`
}

// block is one parsed `resource`/`data`/`module` HCL block.
type block struct {
	kind      NodeKind
	address   string // e.g. "aws_vpc.main", "data.aws_ami.ubuntu", "module.networking"
	label     string
	dir       string
	filePath  string
	startLine int
	endLine   int
	source    string // module blocks only: raw `source` attribute value
	body      *hclsyntax.Body
}

// parsedRepo is the shared result of walking every .tf file once; both the
// visible resource map and the apply-time edit-scope check are derived from
// it so the HCL parsing logic lives in exactly one place.
type parsedRepo struct {
	// blocksByDir maps directory -> address -> block.
	blocksByDir map[string]map[string]*block
}

// parseRepo parses every .tf file in `files` (path -> raw content, paths
// relative to the repository root using forward slashes).
func parseRepo(files map[string]string) *parsedRepo {
	repo := &parsedRepo{
		blocksByDir: map[string]map[string]*block{},
	}

	for filePath, content := range files {
		if !strings.HasSuffix(filePath, ".tf") {
			continue
		}

		hclFile, diags := hclsyntax.ParseConfig([]byte(content), filePath, hcl.InitialPos)
		if diags.HasErrors() {
			// Skip files TowerCrane can't parse (e.g. .tfvars or malformed
			// HCL) instead of failing the whole map.
			continue
		}

		body, ok := hclFile.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}

		dir := path.Dir(filePath)
		if repo.blocksByDir[dir] == nil {
			repo.blocksByDir[dir] = map[string]*block{}
		}

		for _, b := range body.Blocks {
			parsed, ok := blockFromSyntax(b, dir, filePath)
			if !ok {
				continue
			}
			repo.blocksByDir[dir][parsed.address] = parsed
		}
	}

	return repo
}

func blockFromSyntax(b *hclsyntax.Block, dir, filePath string) (*block, bool) {
	rng := b.Range()
	parsed := &block{
		dir:       dir,
		filePath:  filePath,
		startLine: rng.Start.Line,
		endLine:   rng.End.Line,
		body:      b.Body,
	}

	switch b.Type {
	case "resource":
		if len(b.Labels) != 2 {
			return nil, false
		}
		parsed.kind = KindResource
		parsed.address = fmt.Sprintf("%s.%s", b.Labels[0], b.Labels[1])
		parsed.label = parsed.address
	case "data":
		if len(b.Labels) != 2 {
			return nil, false
		}
		parsed.kind = KindData
		parsed.address = fmt.Sprintf("data.%s.%s", b.Labels[0], b.Labels[1])
		parsed.label = parsed.address
	case "module":
		if len(b.Labels) != 1 {
			return nil, false
		}
		parsed.kind = KindModule
		parsed.address = fmt.Sprintf("module.%s", b.Labels[0])
		parsed.label = parsed.address
		if attr, ok := b.Body.Attributes["source"]; ok {
			val, diags := attr.Expr.Value(nil)
			if !diags.HasErrors() && val.Type().FriendlyName() == "string" {
				parsed.source = val.AsString()
			}
		}
	default:
		return nil, false
	}

	return parsed, true
}

// resolveModuleDir resolves a module block's relative `source` into a
// directory path within the repository.
func resolveModuleDir(callerDir, source string) (string, bool) {
	if source == "" || (len(source) > 0 && source[0] != '.') {
		// Registry/remote module sources ("hashicorp/consul/aws", git URLs,
		// etc.) aren't part of this repository, so there's nothing to map.
		return "", false
	}
	return path.Clean(path.Join(callerDir, source)), true
}

// variables collects every symbol referenced anywhere in a block's body,
// including inside nested blocks (e.g. an `ingress { }` block nested in a
// security group resource).
func variables(body *hclsyntax.Body) []hcl.Traversal {
	var out []hcl.Traversal
	for _, attr := range body.Attributes {
		out = append(out, attr.Expr.Variables()...)
	}
	for _, nested := range body.Blocks {
		out = append(out, variables(nested.Body)...)
	}
	return out
}

// referencedAddress interprets a traversal's root as a reference to another
// block in the same directory (resource, data source, or module call).
func referencedAddress(t hcl.Traversal) (string, bool) {
	if len(t) == 0 {
		return "", false
	}

	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return "", false
	}

	switch root.Name {
	case "var", "local", "path", "terraform", "each", "count":
		return "", false
	case "module":
		if len(t) < 2 {
			return "", false
		}
		if attr, ok := t[1].(hcl.TraverseAttr); ok {
			return "module." + attr.Name, true
		}
		return "", false
	case "data":
		if len(t) < 3 {
			return "", false
		}
		typ, ok1 := t[1].(hcl.TraverseAttr)
		name, ok2 := t[2].(hcl.TraverseAttr)
		if !ok1 || !ok2 {
			return "", false
		}
		return fmt.Sprintf("data.%s.%s", typ.Name, name.Name), true
	default:
		if len(t) < 2 {
			return "", false
		}
		if attr, ok := t[1].(hcl.TraverseAttr); ok {
			return root.Name + "." + attr.Name, true
		}
		return "", false
	}
}

// BuildGraph builds the resource map for every block in files
// (repository-relative path -> raw content).
func BuildGraph(files map[string]string) (*Graph, error) {
	repo := parseRepo(files)

	rootDirs := computeRootDirs(repo)

	// nodeID is unique per directory+address; visibleIDs tracks which ones
	// actually made it onto the map.
	nodeID := func(dir, address string) string { return dir + "::" + address }

	graph := &Graph{RootDirs: rootDirs}
	visible := map[string]Node{}

	for dir, blocks := range repo.blocksByDir {
		for addr, b := range blocks {
			id := nodeID(dir, addr)
			n := Node{
				ID:           id,
				Address:      addr,
				Label:        b.label,
				Kind:         b.kind,
				Dir:          dir,
				FilePath:     b.filePath,
				StartLine:    b.startLine,
				EndLine:      b.endLine,
				Applications: []string{},
			}
			visible[id] = n
		}
	}

	// "contains" edges + parent linkage for module calls. A module shared by
	// several callers (e.g. dev and prod) has one contains edge per caller
	// but no single ParentID.
	parents := map[string][]string{}
	for dir, blocks := range repo.blocksByDir {
		for addr, b := range blocks {
			if b.kind != KindModule {
				continue
			}
			moduleID := nodeID(dir, addr)
			if _, ok := visible[moduleID]; !ok {
				continue
			}
			targetDir, ok := resolveModuleDir(dir, b.source)
			if !ok {
				continue
			}
			for childAddr, childBlock := range repo.blocksByDir[targetDir] {
				childID := nodeID(targetDir, childAddr)
				_, ok := visible[childID]
				if !ok {
					continue
				}
				if childBlock.kind != KindModule {
					parents[childID] = append(parents[childID], moduleID)
				}
				graph.Edges = append(graph.Edges, Edge{From: moduleID, To: childID, Kind: EdgeContains})
			}
		}
	}

	for childID, ps := range parents {
		if len(ps) == 1 {
			child := visible[childID]
			child.ParentID = ps[0]
			visible[childID] = child
		}
	}

	// "depends-on" edges from reference scanning.
	for dir, blocks := range repo.blocksByDir {
		for addr, b := range blocks {
			toID := nodeID(dir, addr)
			if _, ok := visible[toID]; !ok {
				continue
			}
			for _, t := range variables(b.body) {
				refAddr, ok := referencedAddress(t)
				if !ok || refAddr == addr {
					continue
				}
				fromID := nodeID(dir, refAddr)
				if _, ok := visible[fromID]; !ok {
					continue
				}
				graph.Edges = append(graph.Edges, Edge{From: fromID, To: toID, Kind: EdgeDependsOn})
			}
		}
	}

	for _, n := range visible {
		graph.Nodes = append(graph.Nodes, n)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		return graph.Edges[i].To < graph.Edges[j].To
	})

	return graph, nil
}

// computeRootDirs returns every directory containing .tf files that is never
// referenced as another module's source - i.e. it looks like an environment
// entry point rather than a reusable module. This ignores visibility: it is
// purely about repository structure.
func computeRootDirs(repo *parsedRepo) []string {
	referenced := map[string]bool{}
	for dir, blocks := range repo.blocksByDir {
		for _, b := range blocks {
			if b.kind != KindModule {
				continue
			}
			if target, ok := resolveModuleDir(dir, b.source); ok {
				referenced[target] = true
			}
		}
	}

	var roots []string
	for dir := range repo.blocksByDir {
		if !referenced[dir] {
			roots = append(roots, dir)
		}
	}
	sort.Strings(roots)
	return roots
}

// FilterByRoot keeps only nodes reachable (ignoring edge direction) from the
// given root directory, plus the edges between them. Used when the frontend
// asks to see a single environment instead of the whole repository.
func (g *Graph) FilterByRoot(root string) *Graph {
	if root == "" {
		return g
	}

	keep := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Dir == root {
			keep[n.ID] = true
		}
	}

	// Expand to connected nodes until no more are added.
	for changed := true; changed; {
		changed = false
		for _, e := range g.Edges {
			if keep[e.From] && !keep[e.To] {
				keep[e.To] = true
				changed = true
			}
			if keep[e.To] && !keep[e.From] {
				keep[e.From] = true
				changed = true
			}
		}
	}

	filtered := &Graph{RootDirs: g.RootDirs}
	for _, n := range g.Nodes {
		if keep[n.ID] {
			filtered.Nodes = append(filtered.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			filtered.Edges = append(filtered.Edges, e)
		}
	}
	return filtered
}

// SourceLines returns lines [start, end] (1-based, inclusive) of content.
func SourceLines(content string, start, end int) string {
	lines := strings.Split(content, "\n")
	if start < 1 || start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
