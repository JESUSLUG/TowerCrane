package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
)

// A Project is where TowerCrane works: its configuration repository (the
// one with .towercrane/: Applications, Groups, Jobs) plus the repositories
// holding the code its Applications run (spec.source.repo). A project's ID
// is its configuration repository's ID, so everything already keyed by
// repository (operations, rollouts, jobs, cloud accounts) belongs to the
// project. Connected repositories are the inventory; a project picks from
// it.

type Project struct {
	ID   string `json:"id"` // = configuration repository ID
	Name string `json:"name"`
	// ManifestsDir is the folder of the configuration repository with the
	// manifests: ".towercrane" (default) or "" for the whole repository.
	ManifestsDir string `json:"manifests_dir"`
	// ManifestFile is the Project manifest (kind: Project) that defines the
	// project ("" for a project created by hand).
	ManifestFile string `json:"manifest_file,omitempty"`
	// RequestedCloudAccounts: what the Project manifest asks for (names);
	// only the ones an admin authorized are usable.
	RequestedCloudAccounts []string `json:"requested_cloud_accounts"`
	// MissingSources: repositories the manifest lists that aren't connected.
	MissingSources []string `json:"missing_sources"`
	CreatedAt time.Time          `json:"created_at"`
	Config    *models.Repository `json:"config_repository"`
	// Sources are the repositories its Applications may take code from
	// (besides the configuration repository itself).
	Sources []*models.Repository `json:"source_repositories"`
}

type ProjectStore struct {
	db    *sql.DB
	repos *RepositoryStore
}

func NewProjectStore(db *sql.DB, repos *RepositoryStore) *ProjectStore {
	return &ProjectStore{db: db, repos: repos}
}

// CreateTables creates the project tables. The first time, every connected
// repository becomes a project of its own (what TowerCrane was before
// projects existed).
func (s *ProjectStore) CreateTables() error {
	var existed bool
	if err := s.db.QueryRow(`SELECT to_regclass('public.projects') IS NOT NULL`).Scan(&existed); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS projects (
		repository_id UUID PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		created_at TIMESTAMP NOT NULL
	);
	CREATE TABLE IF NOT EXISTS project_sources (
		project_id UUID NOT NULL REFERENCES projects(repository_id) ON DELETE CASCADE,
		repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
		PRIMARY KEY (project_id, repository_id)
	);
	ALTER TABLE projects ADD COLUMN IF NOT EXISTS manifests_dir VARCHAR(255) NOT NULL DEFAULT '.towercrane';
	ALTER TABLE projects ADD COLUMN IF NOT EXISTS manifest_file TEXT;
	ALTER TABLE projects ADD COLUMN IF NOT EXISTS requested_accounts TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE projects ADD COLUMN IF NOT EXISTS missing_sources TEXT NOT NULL DEFAULT '[]';`); err != nil {
		return err
	}
	if !existed {
		_, err := s.db.Exec(`INSERT INTO projects (repository_id, name, created_at)
			SELECT id, name, $1 FROM repositories ON CONFLICT DO NOTHING`, time.Now().UTC())
		return err
	}
	return nil
}

func (s *ProjectStore) Create(configRepoID, name string, layout manifest.Layout) error {
	_, err := s.db.Exec(`INSERT INTO projects (repository_id, name, created_at, manifests_dir) VALUES ($1, $2, $3, $4)`,
		configRepoID, name, time.Now().UTC(), layout.Dir)
	return err
}

func (s *ProjectStore) SetLayout(id string, layout manifest.Layout) error {
	_, err := s.db.Exec(`UPDATE projects SET manifests_dir = $2 WHERE repository_id = $1`, id, layout.Dir)
	return err
}

// Layout returns where a project keeps its manifests; a repository that
// isn't a project's configuration uses the default (.towercrane/).
func (s *ProjectStore) Layout(id string) manifest.Layout {
	var dir string
	if err := s.db.QueryRow(`SELECT manifests_dir FROM projects WHERE repository_id = $1`, id).Scan(&dir); err != nil {
		return manifest.DefaultLayout()
	}
	return manifest.Layout{Dir: dir}
}

func (s *ProjectStore) Rename(id, name string) error {
	res, err := s.db.Exec(`UPDATE projects SET name = $2 WHERE repository_id = $1`, id, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *ProjectStore) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM projects WHERE repository_id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Get returns a project with its repositories.
func (s *ProjectStore) Get(id string) (*Project, error) {
	p := &Project{}
	if err := scanProject(s.db.QueryRow(`SELECT repository_id, name, created_at, manifests_dir, COALESCE(manifest_file, ''), requested_accounts, missing_sources FROM projects WHERE repository_id = $1`, id), p); err != nil {
		return nil, err
	}
	return p, s.fill(p)
}

// List returns every project with its repositories.
func (s *ProjectStore) List() ([]*Project, error) {
	rows, err := s.db.Query(`SELECT repository_id, name, created_at, manifests_dir, COALESCE(manifest_file, ''), requested_accounts, missing_sources FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []*Project
	for rows.Next() {
		p := &Project{}
		if err := scanProject(rows, p); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, p := range out {
		if err := s.fill(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *ProjectStore) fill(p *Project) error {
	cfg, err := s.repos.GetRepositoryByID(p.ID)
	if err != nil {
		return err
	}
	p.Config = cfg
	p.Sources, err = s.Sources(p.ID)
	return err
}

// Sources lists a project's source repositories.
func (s *ProjectStore) Sources(projectID string) ([]*models.Repository, error) {
	rows, err := s.db.Query(`SELECT repository_id FROM project_sources WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []*models.Repository{}
	for _, id := range ids {
		r, err := s.repos.GetRepositoryByID(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// SetSources replaces a project's source repositories.
func (s *ProjectStore) SetSources(projectID string, repoIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM project_sources WHERE project_id = $1`, projectID); err != nil {
		return err
	}
	for _, id := range repoIDs {
		if id == projectID {
			continue // the configuration repository is always a source
		}
		if _, err := tx.Exec(`INSERT INTO project_sources (project_id, repository_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			projectID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ProjectsUsing returns the projects a repository belongs to: as their
// configuration repository or as a source.
func (s *ProjectStore) ProjectsUsing(repoID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT repository_id FROM projects WHERE repository_id = $1
		UNION SELECT project_id FROM project_sources WHERE repository_id = $1`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// IsProject reports whether a repository is a project's configuration
// repository.
func (s *ProjectStore) IsProject(repoID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE repository_id = $1`, repoID).Scan(&n)
	return n > 0, err
}

func scanProject(row interface{ Scan(...any) error }, p *Project) error {
	var requested, missing string
	if err := row.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.ManifestsDir, &p.ManifestFile, &requested, &missing); err != nil {
		return err
	}
	p.RequestedCloudAccounts, p.MissingSources = []string{}, []string{}
	_ = json.Unmarshal([]byte(requested), &p.RequestedCloudAccounts)
	_ = json.Unmarshal([]byte(missing), &p.MissingSources)
	return nil
}

// ProjectFromManifest is what a Project manifest says, resolved against the
// connected repositories.
type ProjectFromManifest struct {
	Name                   string
	Layout                 manifest.Layout
	ManifestFile           string
	SourceIDs              []string
	MissingSources         []string
	RequestedCloudAccounts []string
}

// SyncFromManifest creates or updates the project of a configuration
// repository from its Project manifest. Returns whether it was created.
func (s *ProjectStore) SyncFromManifest(repoID string, m ProjectFromManifest) (bool, error) {
	requested, _ := json.Marshal(nonNilStrings(m.RequestedCloudAccounts))
	missing, _ := json.Marshal(nonNilStrings(m.MissingSources))
	res, err := s.db.Exec(`INSERT INTO projects (repository_id, name, created_at, manifests_dir, manifest_file, requested_accounts, missing_sources)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (repository_id) DO NOTHING`,
		repoID, m.Name, time.Now().UTC(), m.Layout.Dir, m.ManifestFile, string(requested), string(missing))
	if err != nil {
		return false, err
	}
	created, _ := res.RowsAffected()
	if created == 0 {
		if _, err := s.db.Exec(`UPDATE projects SET name = $2, manifests_dir = $3, manifest_file = $4,
			requested_accounts = $5, missing_sources = $6 WHERE repository_id = $1`,
			repoID, m.Name, m.Layout.Dir, m.ManifestFile, string(requested), string(missing)); err != nil {
			return false, err
		}
	}
	return created > 0, s.SetSources(repoID, m.SourceIDs)
}

// ManifestProjects lists the IDs of projects defined by a Project manifest.
func (s *ProjectStore) ManifestProjects() ([]string, error) {
	rows, err := s.db.Query(`SELECT repository_id FROM projects WHERE COALESCE(manifest_file, '') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
