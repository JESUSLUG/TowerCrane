package models

import "time"

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	URL           string `json:"url"`
	Owner         string `json:"owner"`
	Description   string `json:"description"`
	Private       bool   `json:"private"`
	GitHubID      int64  `json:"github_id"`
	DefaultBranch string `json:"default_branch"`
	ConnectedBy   string `json:"connected_by"`
	// InstallationID is the GitHub App installation used to access the
	// repository; 0 for repositories connected with a personal OAuth token.
	InstallationID int64     `json:"installation_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type RepositoryBranch struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	Name         string    `json:"name"`
	Commit       string    `json:"commit"`
	IsDefault    bool      `json:"is_default"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RepositoryFile struct {
	ID           string    `json:"id"`
	RepositoryID string    `json:"repository_id"`
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	Type         string    `json:"type"` // file, dir
	Branch       string    `json:"branch"`
	Commit       string    `json:"commit"`
	Content      string    `json:"content,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	RepositoryID string    `json:"repository_id"`
	Path         string    `json:"path"` // Path within repo (e.g., "environments/dev")
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
