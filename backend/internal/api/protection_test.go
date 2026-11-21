package api

import (
	"strings"
	"testing"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
)

func TestCodeownersFor(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"", ""},
		{"* @org/all", "@org/all"},
		{"/.towercrane/ @org/platform", "@org/platform"},
		{".towercrane/ @org/platform", "@org/platform"},
		{"/.towercrane/** @a @b", "@a @b"},
		{"/.towercrane/*.yaml @a", "@a"},
		{"/.towercrane @a", "@a"},
		{"*.tf @infra", ""},
		{"/modules/ @infra", ""},
		{"* @org/all\n/.towercrane/ @org/platform # policy", "@org/platform"},
		{"/.towercrane/ @org/platform\n/.towercrane/", ""}, // last match without owners: nobody
	}
	for _, c := range cases {
		got := strings.Join(codeownersFor(c.content, ".towercrane/app.yaml"), " ")
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.content, got, c.want)
		}
	}
	if strings.Join(codeownersFor("/.towercrane/*.yaml @a", ".towercrane/groups/x.yaml"), "") != "" {
		t.Error("* must not cross directories")
	}
}

func TestCodeownersCoverage(t *testing.T) {
	if _, uncovered := codeownersCoverage("/.towercrane/ @org/platform", manifest.DefaultLayout()); len(uncovered) != 0 {
		t.Errorf("a directory rule covers every manifest, got %v", uncovered)
	}
	_, uncovered := codeownersCoverage("/.towercrane/*.yaml @org/platform", manifest.DefaultLayout())
	if strings.Join(uncovered, ",") != ".towercrane/sandbox/,.towercrane/groups/,.towercrane/diagrams/,.towercrane/jobs/" {
		t.Errorf("*.yaml misses the subfolders, got %v", uncovered)
	}
	if _, uncovered := codeownersCoverage("/.towercrane/ @org/platform\n/.towercrane/jobs/", manifest.DefaultLayout()); strings.Join(uncovered, ",") != ".towercrane/jobs/" {
		t.Errorf("an owner-less rule un-owns jobs/, got %v", uncovered)
	}
}

func TestCodeownersCoverageRoot(t *testing.T) {
	root := manifest.Layout{}
	if _, uncovered := codeownersCoverage("* @org/platform", root); len(uncovered) != 0 {
		t.Errorf("* covers a configuration-only repository: %v", uncovered)
	}
	if _, uncovered := codeownersCoverage("/.towercrane/ @org/platform", root); len(uncovered) == 0 {
		t.Error("a .towercrane/ rule doesn't cover manifests at the root")
	}
}
