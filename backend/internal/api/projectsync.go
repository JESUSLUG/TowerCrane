package api

import (
	"context"
	"log"
	"strings"
	"sync"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

// Projects are manifests too: a connected repository with a Project manifest
// (kind: Project) is a project's configuration. Whenever TowerCrane reads a
// new commit of a repository (on connect, poll, webhook or Refresh), the
// project is created or updated from that manifest: its name, its manifests
// folder (where project.yaml lives) and its code repositories. Removing the
// manifest removes the project (nothing in the cloud is touched). Cloud
// accounts are only requested by the manifest: an admin authorizes each one.
type ProjectSync struct {
	projects  *db.ProjectStore
	repoStore *db.RepositoryStore
	snapshots *SnapshotStore
	gh        *GitHubAccess
	mu        sync.Mutex
}

func NewProjectSync(projects *db.ProjectStore, repoStore *db.RepositoryStore, snapshots *SnapshotStore, gh *GitHubAccess) *ProjectSync {
	return &ProjectSync{projects: projects, repoStore: repoStore, snapshots: snapshots, gh: gh}
}

// Sync applies a repository's Project manifest (from one of its commits).
func (ps *ProjectSync) Sync(repo *models.Repository, files map[string]string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	pm, err := manifest.FindProject(files)
	if err != nil {
		log.Printf("project: %s: %v (project left as it was)", repo.FullName, err)
		return
	}
	if pm == nil {
		// A project created from a manifest goes away with it.
		if p, err := ps.projects.Get(repo.ID); err == nil && p.ManifestFile != "" {
			if err := ps.projects.Delete(repo.ID); err == nil {
				log.Printf("project: %s: %s was removed, project %q removed (the cloud is untouched)", repo.FullName, p.ManifestFile, p.Name)
			}
		}
		return
	}

	connected, err := ps.repoStore.ListRepositories()
	if err != nil {
		log.Printf("project: %s: %v", repo.FullName, err)
		return
	}
	byName := map[string]string{}
	for _, r := range connected {
		byName[strings.ToLower(r.FullName)] = r.ID
	}
	m := db.ProjectFromManifest{
		Name: pm.Metadata.Name, Layout: pm.Layout(), ManifestFile: pm.File,
		RequestedCloudAccounts: pm.Spec.CloudAccounts,
	}
	for _, src := range pm.Spec.Sources {
		if id, ok := byName[strings.ToLower(src)]; ok {
			m.SourceIDs = append(m.SourceIDs, id)
		} else {
			m.MissingSources = append(m.MissingSources, src)
		}
	}
	created, err := ps.projects.SyncFromManifest(repo.ID, m)
	if err != nil {
		log.Printf("project: %s: %v", repo.FullName, err)
		return
	}
	if created {
		log.Printf("project: %s: project %q created from %s", repo.FullName, m.Name, pm.File)
	}
}

// ResyncAll re-applies every Project manifest (e.g. after a repository was
// connected: a project may have been waiting for it as a code repository).
func (ps *ProjectSync) ResyncAll(ctx context.Context) {
	ids, err := ps.projects.ManifestProjects()
	if err != nil {
		log.Printf("project: %v", err)
		return
	}
	for _, id := range ids {
		repo, err := ps.repoStore.GetRepositoryByID(id)
		if err != nil {
			continue
		}
		snap := ps.snapshots.Get(id)
		if snap == nil {
			if snap, err = ps.snapshots.readSnapshot(ctx, ps.gh, repo, ""); err != nil {
				continue // readSnapshot synced it already if it could read it
			}
		}
		ps.Sync(repo, snap.Files)
	}
}

// Connected handles a newly connected repository: reads it (creating its
// project if it declares one) and re-checks the other projects.
func (ps *ProjectSync) Connected(repo *models.Repository) {
	ctx := context.Background()
	if _, err := ps.snapshots.readSnapshot(ctx, ps.gh, repo, ""); err != nil {
		log.Printf("project: %s: %v", repo.FullName, err)
	}
	ps.ResyncAll(ctx)
}
