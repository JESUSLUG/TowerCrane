package db

import (
	"database/sql"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/models"
	"github.com/google/uuid"
)

type RepositoryStore struct {
	db *sql.DB
}

func NewRepositoryStore(db *sql.DB) *RepositoryStore {
	return &RepositoryStore{db: db}
}

func (s *RepositoryStore) CreateTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS repositories (
		id UUID PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		full_name VARCHAR(255) UNIQUE NOT NULL,
		url VARCHAR(512) NOT NULL,
		owner VARCHAR(255) NOT NULL,
		description TEXT,
		private BOOLEAN NOT NULL,
		github_id INTEGER UNIQUE NOT NULL,
		default_branch VARCHAR(255),
		connected_by UUID NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (connected_by) REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS repository_branches (
		id UUID PRIMARY KEY,
		repository_id UUID NOT NULL,
		name VARCHAR(255) NOT NULL,
		commit VARCHAR(40) NOT NULL,
		is_default BOOLEAN NOT NULL DEFAULT false,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
		UNIQUE(repository_id, name)
	);

	CREATE TABLE IF NOT EXISTS repository_files (
		id UUID PRIMARY KEY,
		repository_id UUID NOT NULL,
		path VARCHAR(1024) NOT NULL,
		name VARCHAR(255) NOT NULL,
		type VARCHAR(50) NOT NULL,
		branch VARCHAR(255) NOT NULL,
		commit VARCHAR(40) NOT NULL,
		content TEXT,
		updated_at TIMESTAMP NOT NULL,
		FOREIGN KEY (repository_id) REFERENCES repositories(id) ON DELETE CASCADE,
		UNIQUE(repository_id, path, branch)
	);

	CREATE INDEX IF NOT EXISTS idx_repo_owner ON repositories(owner);
	CREATE INDEX IF NOT EXISTS idx_repo_github_id ON repositories(github_id);
	CREATE INDEX IF NOT EXISTS idx_repo_branch_repo_id ON repository_branches(repository_id);
	CREATE INDEX IF NOT EXISTS idx_repo_file_repo_id ON repository_files(repository_id);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Additive migrations for databases created by older versions.
	for _, m := range []string{
		// GitHub ids no longer fit in 32 bits.
		`ALTER TABLE repositories ALTER COLUMN github_id TYPE BIGINT`,
		// GitHub App installation used to access the repository (NULL for
		// repositories connected with a personal OAuth token).
		`ALTER TABLE repositories ADD COLUMN IF NOT EXISTS installation_id BIGINT`,
		// Single cloud account of older versions (now
		// repository_cloud_accounts; migrated by IntegrationStore).
		`ALTER TABLE repositories ADD COLUMN IF NOT EXISTS cloud_account_id UUID`,
		// Last default-branch commit TowerCrane reacted to (from a webhook
		// or from polling), so the same commit is never processed twice.
		`ALTER TABLE repositories ADD COLUMN IF NOT EXISTS last_seen_sha VARCHAR(40)`,
	} {
		if _, err := s.db.Exec(m); err != nil {
			return err
		}
	}
	return nil
}

func (s *RepositoryStore) CreateRepository(repo *models.Repository) (*models.Repository, error) {
	repo.ID = uuid.New().String()
	repo.CreatedAt = time.Now()
	repo.UpdatedAt = time.Now()

	query := `
	INSERT INTO repositories (id, name, full_name, url, owner, description, private, github_id, default_branch, connected_by, created_at, updated_at, installation_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, 0))
	RETURNING ` + repoColumns

	row := s.db.QueryRow(query, repo.ID, repo.Name, repo.FullName, repo.URL, repo.Owner, repo.Description, repo.Private, repo.GitHubID, repo.DefaultBranch, repo.ConnectedBy, repo.CreatedAt, repo.UpdatedAt, repo.InstallationID)
	return s.scanRepository(row)
}

func (s *RepositoryStore) GetRepositoryByFullName(fullName string) (*models.Repository, error) {
	query := `SELECT ` + repoColumns + ` FROM repositories WHERE full_name = $1`
	row := s.db.QueryRow(query, fullName)
	return s.scanRepository(row)
}

func (s *RepositoryStore) GetRepositoryByID(id string) (*models.Repository, error) {
	query := `SELECT ` + repoColumns + ` FROM repositories WHERE id = $1`
	row := s.db.QueryRow(query, id)
	return s.scanRepository(row)
}

func (s *RepositoryStore) ListRepositories() ([]*models.Repository, error) {
	query := `SELECT ` + repoColumns + ` FROM repositories ORDER BY created_at DESC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []*models.Repository
	for rows.Next() {
		repo, err := s.scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}

	return repos, rows.Err()
}

func (s *RepositoryStore) ListRepositoriesByOwner(owner string) ([]*models.Repository, error) {
	query := `SELECT ` + repoColumns + ` FROM repositories WHERE owner = $1 ORDER BY created_at DESC`
	rows, err := s.db.Query(query, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repos []*models.Repository
	for rows.Next() {
		repo, err := s.scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}

	return repos, rows.Err()
}

func (s *RepositoryStore) scanRepository(row interface {
	Scan(...interface{}) error
}) (*models.Repository, error) {
	repo := &models.Repository{}
	var description sql.NullString
	var installationID sql.NullInt64
	err := row.Scan(&repo.ID, &repo.Name, &repo.FullName, &repo.URL, &repo.Owner, &description, &repo.Private, &repo.GitHubID, &repo.DefaultBranch, &repo.ConnectedBy, &repo.CreatedAt, &repo.UpdatedAt, &installationID)
	if err != nil {
		return nil, err
	}
	repo.Description = description.String
	repo.InstallationID = installationID.Int64
	return repo, nil
}

const repoColumns = `id, name, full_name, url, owner, description, private, github_id, default_branch, connected_by, created_at, updated_at, installation_id`

// ClaimCommit records sha as the latest default-branch commit handled for a
// repository. It returns true only for the first caller that reports this
// commit, so a push seen by both the webhook and the poller runs once.
func (s *RepositoryStore) ClaimCommit(repoID, sha string) (bool, error) {
	res, err := s.db.Exec(`UPDATE repositories SET last_seen_sha = $1
		WHERE id = $2 AND last_seen_sha IS DISTINCT FROM $1`, sha, repoID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// InitLastSeen sets the baseline commit for a repository that has none yet
// (e.g. just connected). It reports whether it did: such a commit is a
// starting point, not a new change to react to.
func (s *RepositoryStore) InitLastSeen(repoID, sha string) (bool, error) {
	res, err := s.db.Exec(`UPDATE repositories SET last_seen_sha = $1 WHERE id = $2 AND last_seen_sha IS NULL`, sha, repoID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ClearInstallation detaches repositories from a removed GitHub App
// installation (they stay connected but can't be read until re-linked).
func (s *RepositoryStore) ClearInstallation(installationID int64) error {
	_, err := s.db.Exec(`UPDATE repositories SET installation_id = NULL, updated_at = $1 WHERE installation_id = $2`, time.Now(), installationID)
	return err
}

// Branches

func (s *RepositoryStore) CreateBranch(branch *models.RepositoryBranch) (*models.RepositoryBranch, error) {
	branch.ID = uuid.New().String()
	branch.UpdatedAt = time.Now()

	query := `
	INSERT INTO repository_branches (id, repository_id, name, commit, is_default, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (repository_id, name) DO UPDATE
	SET commit = $4, updated_at = $6
	RETURNING id, repository_id, name, commit, is_default, updated_at
	`

	row := s.db.QueryRow(query, branch.ID, branch.RepositoryID, branch.Name, branch.Commit, branch.IsDefault, branch.UpdatedAt)
	return s.scanBranch(row)
}

func (s *RepositoryStore) ListBranches(repoID string) ([]*models.RepositoryBranch, error) {
	query := `SELECT id, repository_id, name, commit, is_default, updated_at FROM repository_branches WHERE repository_id = $1 ORDER BY name`
	rows, err := s.db.Query(query, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var branches []*models.RepositoryBranch
	for rows.Next() {
		branch, err := s.scanBranch(rows)
		if err != nil {
			return nil, err
		}
		branches = append(branches, branch)
	}

	return branches, rows.Err()
}

func (s *RepositoryStore) scanBranch(row interface {
	Scan(...interface{}) error
}) (*models.RepositoryBranch, error) {
	branch := &models.RepositoryBranch{}
	err := row.Scan(&branch.ID, &branch.RepositoryID, &branch.Name, &branch.Commit, &branch.IsDefault, &branch.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return branch, nil
}

// Files

func (s *RepositoryStore) CreateFile(file *models.RepositoryFile) (*models.RepositoryFile, error) {
	file.ID = uuid.New().String()
	file.UpdatedAt = time.Now()

	query := `
	INSERT INTO repository_files (id, repository_id, path, name, type, branch, commit, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (repository_id, path, branch) DO UPDATE
	SET commit = $7, updated_at = $8
	RETURNING id, repository_id, path, name, type, branch, commit, updated_at
	`

	row := s.db.QueryRow(query, file.ID, file.RepositoryID, file.Path, file.Name, file.Type, file.Branch, file.Commit, file.UpdatedAt)
	return s.scanFile(row)
}

func (s *RepositoryStore) ListFiles(repoID, branch, path string) ([]*models.RepositoryFile, error) {
	query := `SELECT id, repository_id, path, name, type, branch, commit, updated_at FROM repository_files 
	WHERE repository_id = $1 AND branch = $2 AND path LIKE $3 || '%' 
	ORDER BY path`
	rows, err := s.db.Query(query, repoID, branch, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*models.RepositoryFile
	for rows.Next() {
		file, err := s.scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	return files, rows.Err()
}

func (s *RepositoryStore) GetFile(repoID, branch, path string) (*models.RepositoryFile, error) {
	query := `SELECT id, repository_id, path, name, type, branch, commit, updated_at FROM repository_files 
	WHERE repository_id = $1 AND branch = $2 AND path = $3`
	row := s.db.QueryRow(query, repoID, branch, path)
	return s.scanFile(row)
}

func (s *RepositoryStore) scanFile(row interface {
	Scan(...interface{}) error
}) (*models.RepositoryFile, error) {
	file := &models.RepositoryFile{}
	err := row.Scan(&file.ID, &file.RepositoryID, &file.Path, &file.Name, &file.Type, &file.Branch, &file.Commit, &file.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return file, nil
}
