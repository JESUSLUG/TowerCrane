package tfmap

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// loadExampleRepo reads every .tf file from the cloned TowerCrane-Example
// fixture so tests can exercise the real repository structure.
func loadExampleRepo(t *testing.T, _ ...string) map[string]string {
	t.Helper()

	root := "/tmp/TowerCrane-Example"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("TowerCrane-Example fixture not available at %s: %v", root, err)
	}

	files := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".tf") {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}
	return files
}

func TestBuildGraph_RootDirsDetected(t *testing.T) {
	files := loadExampleRepo(t)
	graph, err := BuildGraph(files)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	want := []string{"environments/dev", "environments/prod"}
	sort.Strings(graph.RootDirs)
	if strings.Join(graph.RootDirs, ",") != strings.Join(want, ",") {
		t.Fatalf("RootDirs = %v, want %v", graph.RootDirs, want)
	}
}

func TestBuildGraph_EveryBlockIsANode(t *testing.T) {
	files := loadExampleRepo(t)
	graph, err := BuildGraph(files)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("expected every block to be on the map, got none")
	}
	for _, n := range graph.Nodes {
		if n.StartLine == 0 || n.FilePath == "" {
			t.Errorf("%s: missing location", n.ID)
		}
		if n.Source != "" || n.Editable {
			t.Errorf("%s: source/editable must only be set from manifests", n.ID)
		}
	}
}

func TestBuildGraph_VisibleModuleTree(t *testing.T) {
	files := loadExampleRepo(t,
		"environments/dev",
		"modules/networking",
		"modules/compute",
		"modules/database",
	)

	graph, err := BuildGraph(files)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	byID := map[string]Node{}
	for _, n := range graph.Nodes {
		byID[n.ID] = n
	}

	// The three module calls declared in environments/dev/main.tf must exist.
	for _, addr := range []string{"module.networking", "module.compute", "module.database"} {
		id := "environments/dev::" + addr
		if _, ok := byID[id]; !ok {
			t.Errorf("expected module node %s to be visible", id)
		}
	}

	// A resource nested inside the networking module must exist and be
	// parented to the module-call node, reached through a "contains" edge.
	vpcID := "modules/networking::aws_vpc.main"
	vpc, ok := byID[vpcID]
	if !ok {
		t.Fatalf("expected %s to be visible", vpcID)
	}
	// networking is shared by dev and prod, so it has no single parent.
	if vpc.ParentID != "" {
		t.Errorf("vpc.ParentID = %q, want empty for a module shared by several environments", vpc.ParentID)
	}

	foundContains := false
	for _, e := range graph.Edges {
		if e.Kind == EdgeContains && e.From == "environments/dev::module.networking" && e.To == vpcID {
			foundContains = true
		}
	}
	if !foundContains {
		t.Errorf("expected a contains edge from module.networking to %s", vpcID)
	}

	// aws_subnet.public references aws_vpc.main.id, so a depends-on edge
	// should connect them within the networking module.
	subnetID := "modules/networking::aws_subnet.public"
	foundDep := false
	for _, e := range graph.Edges {
		if e.Kind == EdgeDependsOn && e.From == vpcID && e.To == subnetID {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("expected depends-on edge from %s to %s", vpcID, subnetID)
	}

	// environments/dev/main.tf wires compute's security_group_ids from
	// module.networking's output, so the two module-call nodes should be
	// linked by a cross-module dependency edge.
	netModID := "environments/dev::module.networking"
	computeModID := "environments/dev::module.compute"
	foundCrossModuleDep := false
	for _, e := range graph.Edges {
		if e.Kind == EdgeDependsOn && e.From == netModID && e.To == computeModID {
			foundCrossModuleDep = true
		}
	}
	if !foundCrossModuleDep {
		t.Errorf("expected depends-on edge from %s to %s", netModID, computeModID)
	}
}

func TestBuildGraph_DataSourceVisible(t *testing.T) {
	files := loadExampleRepo(t, "modules/compute")
	graph, err := BuildGraph(files)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}

	byID := map[string]Node{}
	for _, n := range graph.Nodes {
		byID[n.ID] = n
	}

	dataID := "modules/compute::data.aws_ami.ubuntu"
	data, ok := byID[dataID]
	if !ok {
		t.Fatalf("expected data source node %s to be visible", dataID)
	}
	if data.Kind != KindData {
		t.Errorf("Kind = %q, want %q", data.Kind, KindData)
	}

	instanceID := "modules/compute::aws_instance.main"
	foundDep := false
	for _, e := range graph.Edges {
		if e.Kind == EdgeDependsOn && e.From == dataID && e.To == instanceID {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("expected depends-on edge from %s to %s", dataID, instanceID)
	}
}
