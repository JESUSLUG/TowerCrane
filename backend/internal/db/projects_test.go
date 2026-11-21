package db

import (
	"os"
	"testing"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/google/uuid"
)

// Needs a Postgres: TOWERCRANE_TEST_DB=1 with DB_* pointing at a test database.
func TestProjects(t *testing.T) {
	if os.Getenv("TOWERCRANE_TEST_DB") == "" {
		t.Skip("set TOWERCRANE_TEST_DB=1 and DB_* to run against a test database")
	}
	database, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repos := NewRepositoryStore(database)
	if err := repos.CreateTables(); err != nil {
		t.Fatal(err)
	}
	ps := NewProjectStore(database, repos)
	if err := ps.CreateTables(); err != nil {
		t.Fatal(err)
	}
	user := uuid.NewString()
	if _, err := database.Exec(`INSERT INTO users (id, email, name, created_at, updated_at) VALUES ($1, $2, 'proj-test', $3, $3)`,
		user, user+"@test.invalid", time.Now()); err != nil {
		t.Fatal(err)
	}
	defer database.Exec(`DELETE FROM users WHERE id = $1`, user)
	mk := func(name string) *models.Repository {
		r, err := repos.CreateRepository(&models.Repository{Name: name, FullName: "zz-test/" + name + "-" + uuid.NewString()[:8],
			URL: "https://github.com/zz-test/" + name, Owner: "zz-test", DefaultBranch: "main", GitHubID: time.Now().UnixNano(),
			ConnectedBy: user})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	cfg, infra, apps := mk("platform"), mk("infra"), mk("apps")
	defer database.Exec(`DELETE FROM repositories WHERE id IN ($1, $2, $3)`, cfg.ID, infra.ID, apps.ID) // runs before the user's delete

	if err := ps.Create(cfg.ID, "platform", manifest.Layout{}); err != nil {
		t.Fatal(err)
	}
	if l := ps.Layout(cfg.ID); !l.Root() {
		t.Errorf("manifests at the repository root, got %q", l)
	}
	if l := ps.Layout(apps.ID); l.Dir != ".towercrane" {
		t.Errorf("not a project: default layout, got %q", l)
	}
	if err := ps.SetSources(cfg.ID, []string{infra.ID, apps.ID, cfg.ID}); err != nil {
		t.Fatal(err)
	}
	p, err := ps.Get(cfg.ID)
	if err != nil || p.Config.ID != cfg.ID || len(p.Sources) != 2 {
		t.Fatalf("project = %+v, %v", p, err)
	}
	for _, r := range []*models.Repository{cfg, apps} {
		ids, err := ps.ProjectsUsing(r.ID)
		if err != nil || len(ids) != 1 || ids[0] != cfg.ID {
			t.Errorf("%s used by %v, %v", r.Name, ids, err)
		}
	}
	if is, _ := ps.IsProject(apps.ID); is {
		t.Error("a source repository is not a project")
	}
	// From a Project manifest: created, then updated by a new commit.
	m := ProjectFromManifest{Name: "plat", Layout: manifest.Layout{}, ManifestFile: "project.yaml",
		SourceIDs: []string{infra.ID}, MissingSources: []string{"acme/apps"}, RequestedCloudAccounts: []string{"aws-x"}}
	if created, err := ps.SyncFromManifest(apps.ID, m); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	m.Name, m.MissingSources, m.SourceIDs = "plat2", nil, []string{infra.ID, cfg.ID}
	if created, err := ps.SyncFromManifest(apps.ID, m); err != nil || created {
		t.Fatalf("second sync must update: created=%v err=%v", created, err)
	}
	got, err := ps.Get(apps.ID)
	if err != nil || got.Name != "plat2" || got.ManifestFile != "project.yaml" || len(got.MissingSources) != 0 ||
		len(got.Sources) != 2 || len(got.RequestedCloudAccounts) != 1 {
		t.Fatalf("synced project = %+v, %v", got, err)
	}
	if ids, _ := ps.ManifestProjects(); !contains(ids, apps.ID) || contains(ids, cfg.ID) {
		t.Errorf("manifest projects = %v", ids)
	}
	ps.Delete(apps.ID)

	if err := ps.Delete(cfg.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := ps.ProjectsUsing(apps.ID); len(ids) != 0 {
		t.Errorf("deleting a project drops its sources: %v", ids)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
