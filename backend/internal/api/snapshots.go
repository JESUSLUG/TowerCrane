package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

// TowerCrane reads a repository from GitHub only when it checks the default
// branch: every poll interval (3 minutes by default), on a webhook, or when
// someone presses Refresh. The files of the commit it read (.tf, .hcl and
// .towercrane/ YAML alike) are kept here and every page is built from them,
// so Terraform code and TowerCrane manifests always follow the same rules.
// A commit never changes, so a snapshot only moves on with a new commit.

type repoSnapshot struct {
	SHA    string
	Files  map[string]string
	ReadAt time.Time
}

type SnapshotStore struct {
	mu sync.RWMutex
	m  map[string]*repoSnapshot
	// onNew runs after a new commit of a repository was read (projects are
	// synced from their Project manifest there).
	onNew func(repo *models.Repository, files map[string]string)
}

func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{m: map[string]*repoSnapshot{}}
}

func (s *SnapshotStore) Get(repoID string) *repoSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[repoID]
}

func (s *SnapshotStore) Set(repoID, sha string, files map[string]string) *repoSnapshot {
	snap := &repoSnapshot{SHA: sha, Files: files, ReadAt: time.Now()}
	s.mu.Lock()
	s.m[repoID] = snap
	s.mu.Unlock()
	return snap
}

// readSnapshot reads the default branch's head commit from GitHub and keeps
// it. Used when a commit is processed, on Refresh, and the first time a
// repository is shown after TowerCrane starts.
func (s *SnapshotStore) readSnapshot(ctx context.Context, gh *GitHubAccess, repo *models.Repository, sha string) (*repoSnapshot, error) {
	client, err := gh.Client(ctx, repo)
	if err != nil {
		return nil, err
	}
	if sha == "" {
		branch := repo.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		b, _, err := client.Raw().Repositories.GetBranch(ctx, repo.Owner, repo.Name, branch, 1)
		if err != nil {
			return nil, fmt.Errorf("failed to read branch %s: %w", branch, err)
		}
		sha = b.GetCommit().GetSHA()
	}
	if cur := s.Get(repo.ID); cur != nil && cur.SHA == sha {
		return cur, nil // same commit, same files
	}
	files, err := client.GetAllTerraformFiles(ctx, repo.Owner, repo.Name, sha)
	if err != nil {
		return nil, fmt.Errorf("failed to read repository files: %w", err)
	}
	snap := s.Set(repo.ID, sha, files)
	if s.onNew != nil {
		s.onNew(repo, files)
	}
	return snap, nil
}
