package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JESUSLUG/TowerCrane/backend/internal/secrets"
	"strings"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/google/uuid"
)

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) CreateTables() error {
	createTable := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY,
		github_id INTEGER UNIQUE,
		github_login VARCHAR(255) UNIQUE,
		email VARCHAR(255) UNIQUE,
		name VARCHAR(255),
		avatar_url VARCHAR(512),
		password_hash VARCHAR(255),
		is_local_user BOOLEAN DEFAULT FALSE,
		github_access_token TEXT,
		local_role VARCHAR(20),
		github_teams TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);
	`

	if _, err := s.db.Exec(createTable); err != nil {
		return err
	}

	// Additive migrations for databases created by older versions.
	for _, m := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS github_access_token TEXT`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS local_role VARCHAR(20)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS github_teams TEXT`,
		// Set when the user joined through an invitation: they may sign in
		// even if they aren't in a connected GitHub org.
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS invited BOOLEAN NOT NULL DEFAULT FALSE`,
		// Sessions (JWTs) issued before this moment are refused (sign-out).
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS sessions_revoked_at TIMESTAMP`,
		`ALTER TABLE users ALTER COLUMN github_id TYPE BIGINT`,
	} {
		if _, err := s.db.Exec(m); err != nil {
			return err
		}
	}

	if _, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS role_mappings (
		id UUID PRIMARY KEY,
		github_org VARCHAR(255) NOT NULL,
		team_slug VARCHAR(255) NOT NULL,
		role VARCHAR(20) NOT NULL,
		created_at TIMESTAMP NOT NULL,
		UNIQUE (github_org, team_slug)
	)`); err != nil {
		return err
	}

	// Create indexes
	indexCommands := []string{
		`CREATE INDEX IF NOT EXISTS idx_github_id ON users(github_id)`,
		`CREATE INDEX IF NOT EXISTS idx_github_login ON users(github_login)`,
		`CREATE INDEX IF NOT EXISTS idx_email ON users(email)`,
	}

	for _, cmd := range indexCommands {
		if _, err := s.db.Exec(cmd); err != nil {
			// Ignore errors for indexes that already exist
		}
	}

	return nil
}

func (s *UserStore) GetOrCreateUser(user *auth.User) (*auth.User, error) {
	// Try to get existing user
	if user.GitHubID.Valid {
		existingUser, err := s.GetUserByGitHubID(user.GitHubID.Int64)
		if err == nil {
			return existingUser, nil
		}
	}

	// Create new user
	user.ID = uuid.New().String()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	query := `
	INSERT INTO users (id, github_id, github_login, email, name, avatar_url, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (github_id) DO UPDATE 
	SET email = $4, name = $5, avatar_url = $6, updated_at = $8
	RETURNING id, github_id, github_login, email, name, avatar_url, created_at, updated_at
	`

	var githubID *int64
	if user.GitHubID.Valid {
		githubID = &user.GitHubID.Int64
	}

	var githubLogin *string
	if user.GitHubLogin.Valid {
		githubLogin = &user.GitHubLogin.String
	}

	row := s.db.QueryRow(query, user.ID, githubID, githubLogin, user.Email, user.Name, user.AvatarURL, user.CreatedAt, user.UpdatedAt)
	return s.scanUser(row)
}

func (s *UserStore) GetUserByGitHubID(githubID int64) (*auth.User, error) {
	query := `SELECT id, github_id, github_login, email, name, avatar_url, created_at, updated_at FROM users WHERE github_id = $1`
	row := s.db.QueryRow(query, githubID)
	return s.scanUser(row)
}

func (s *UserStore) GetUserByGitHubLogin(login string) (*auth.User, error) {
	query := `SELECT id, github_id, github_login, email, name, avatar_url, created_at, updated_at FROM users WHERE github_login = $1`
	row := s.db.QueryRow(query, login)
	return s.scanUser(row)
}

func (s *UserStore) GetUserByID(id string) (*auth.User, error) {
	query := `SELECT id, github_id, github_login, email, name, avatar_url, created_at, updated_at FROM users WHERE id = $1`
	row := s.db.QueryRow(query, id)
	return s.scanUser(row)
}

func (s *UserStore) scanUser(row *sql.Row) (*auth.User, error) {
	user := &auth.User{}
	err := row.Scan(&user.ID, &user.GitHubID, &user.GitHubLogin, &user.Email, &user.Name, &user.AvatarURL, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserStore) CreateLocalUser(email, passwordHash, name string) (*auth.User, error) {
	user := &auth.User{
		ID:        uuid.New().String(),
		Email:     email,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	query := `
	INSERT INTO users (id, email, name, password_hash, is_local_user, created_at, updated_at)
	VALUES ($1, $2, $3, $4, TRUE, $5, $6)
	RETURNING id, github_id, github_login, email, name, avatar_url, created_at, updated_at
	`

	row := s.db.QueryRow(query, user.ID, email, name, passwordHash, user.CreatedAt, user.UpdatedAt)
	return s.scanUser(row)
}

func (s *UserStore) GetUserByEmail(email string) (*auth.User, error) {
	query := `SELECT id, github_id, github_login, email, name, avatar_url, created_at, updated_at FROM users WHERE lower(email) = lower($1)`
	row := s.db.QueryRow(query, email)
	return s.scanUser(row)
}

func (s *UserStore) GetUserPasswordHash(userID string) (string, error) {
	query := `SELECT password_hash FROM users WHERE id = $1`
	var hash string
	err := s.db.QueryRow(query, userID).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}

// encryptedTokenPrefix marks a GitHub token encrypted at rest (older
// versions stored them in plaintext).
const encryptedTokenPrefix = "enc:v1:"

// SetGitHubToken persists the user's GitHub OAuth access token, encrypted
// (secrets.Encrypt, TOWERCRANE_ENCRYPTION_KEY), so later requests can call
// the GitHub API without the browser resending it.
func (s *UserStore) SetGitHubToken(userID, token string) error {
	stored := ""
	if token != "" {
		enc, err := secrets.Encrypt(token)
		if err != nil {
			return fmt.Errorf("failed to encrypt the GitHub token: %w", err)
		}
		stored = encryptedTokenPrefix + enc
	}
	_, err := s.db.Exec(`UPDATE users SET github_access_token = NULLIF($1, ''), updated_at = $2 WHERE id = $3`,
		stored, time.Now(), userID)
	return err
}

func (s *UserStore) GetGitHubToken(userID string) (string, error) {
	var token sql.NullString
	if err := s.db.QueryRow(`SELECT github_access_token FROM users WHERE id = $1`, userID).Scan(&token); err != nil {
		return "", err
	}
	if !strings.HasPrefix(token.String, encryptedTokenPrefix) {
		return token.String, nil // legacy plaintext (EncryptGitHubTokens migrates it)
	}
	return secrets.Decrypt(strings.TrimPrefix(token.String, encryptedTokenPrefix))
}

// EncryptGitHubTokens encrypts tokens stored in plaintext by older versions.
func (s *UserStore) EncryptGitHubTokens() (int, error) {
	rows, err := s.db.Query(`SELECT id, github_access_token FROM users
		WHERE github_access_token IS NOT NULL AND github_access_token <> '' AND github_access_token NOT LIKE 'enc:v1:%'`)
	if err != nil {
		return 0, err
	}
	type plain struct{ id, token string }
	var todo []plain
	for rows.Next() {
		var p plain
		if err := rows.Scan(&p.id, &p.token); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		if err := s.SetGitHubToken(p.id, p.token); err != nil {
			return 0, err
		}
	}
	return len(todo), nil
}

// UserWithRoles is a user plus every source its role comes from.
type UserWithRoles struct {
	User  *auth.User
	Roles auth.RoleSources
}

// GetRoleSources returns where a user's role comes from. The GitHub-derived
// role is computed from the user's teams (refreshed at sign-in) and the
// current team mappings, so changing a mapping takes effect immediately.
func (s *UserStore) GetRoleSources(userID string) (auth.RoleSources, error) {
	var local, teams, login sql.NullString
	var email string
	err := s.db.QueryRow(`SELECT local_role, github_teams, github_login, COALESCE(email, '') FROM users WHERE id = $1`, userID).
		Scan(&local, &teams, &login, &email)
	if err != nil {
		return auth.RoleSources{}, err
	}
	mappings, err := s.ListRoleMappings()
	if err != nil {
		return auth.RoleSources{}, err
	}
	return auth.RoleSources{
		Local:     auth.Role(local.String),
		GitHub:    githubRole(teams.String, mappings),
		Bootstrap: auth.IsBootstrapAdmin(login.String, email),
	}, nil
}

// githubRole is the highest role any of the user's teams is mapped to.
func githubRole(teamsJSON string, mappings []RoleMapping) auth.Role {
	var teams []string
	_ = json.Unmarshal([]byte(teamsJSON), &teams)
	member := make(map[string]bool, len(teams))
	for _, t := range teams {
		member[t] = true
	}
	role := auth.RoleNone
	for _, m := range mappings {
		if member[m.GitHubOrg+"/"+m.TeamSlug] {
			role = auth.MaxRole(role, m.Role)
		}
	}
	return role
}

// SetLocalRole sets the role assigned inside TowerCrane.
func (s *UserStore) SetLocalRole(userID string, role auth.Role) error {
	res, err := s.db.Exec(`UPDATE users SET local_role = NULLIF($1, ''), updated_at = $2 WHERE id = $3`, string(role), time.Now(), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetGitHubTeams stores the GitHub teams ("org/slug") the user belongs to.
func (s *UserStore) SetGitHubTeams(userID string, teams []string) error {
	if teams == nil {
		teams = []string{}
	}
	data, _ := json.Marshal(teams)
	_, err := s.db.Exec(`UPDATE users SET github_teams = $1, updated_at = $2 WHERE id = $3`, string(data), time.Now(), userID)
	return err
}

// ListUsersWithRoles returns every user with their role sources.
func (s *UserStore) ListUsersWithRoles() ([]UserWithRoles, error) {
	mappings, err := s.ListRoleMappings()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, github_id, github_login, COALESCE(email, ''), COALESCE(name, ''), avatar_url,
		created_at, updated_at, local_role, github_teams FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserWithRoles
	for rows.Next() {
		u := &auth.User{}
		var local, teams sql.NullString
		if err := rows.Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Email, &u.Name, &u.AvatarURL,
			&u.CreatedAt, &u.UpdatedAt, &local, &teams); err != nil {
			return nil, err
		}
		out = append(out, UserWithRoles{User: u, Roles: auth.RoleSources{
			Local:     auth.Role(local.String),
			GitHub:    githubRole(teams.String, mappings),
			Bootstrap: auth.IsBootstrapAdmin(u.GitHubLogin.String, u.Email),
		}})
	}
	return out, rows.Err()
}

// RoleMapping grants a TowerCrane role to every member of a GitHub team.
type RoleMapping struct {
	ID        string    `json:"id"`
	GitHubOrg string    `json:"github_org"`
	TeamSlug  string    `json:"team_slug"`
	Role      auth.Role `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *UserStore) ListRoleMappings() ([]RoleMapping, error) {
	rows, err := s.db.Query(`SELECT id, github_org, team_slug, role, created_at FROM role_mappings ORDER BY github_org, team_slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RoleMapping
	for rows.Next() {
		var m RoleMapping
		if err := rows.Scan(&m.ID, &m.GitHubOrg, &m.TeamSlug, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpsertRoleMapping creates or updates the role granted to a GitHub team.
func (s *UserStore) UpsertRoleMapping(org, team string, role auth.Role) (*RoleMapping, error) {
	m := &RoleMapping{}
	err := s.db.QueryRow(`
	INSERT INTO role_mappings (id, github_org, team_slug, role, created_at)
	VALUES ($1, lower($2), lower($3), $4, $5)
	ON CONFLICT (github_org, team_slug) DO UPDATE SET role = EXCLUDED.role
	RETURNING id, github_org, team_slug, role, created_at`,
		uuid.New().String(), org, team, string(role), time.Now()).
		Scan(&m.ID, &m.GitHubOrg, &m.TeamSlug, &m.Role, &m.CreatedAt)
	return m, err
}

func (s *UserStore) DeleteRoleMapping(id string) error {
	res, err := s.db.Exec(`DELETE FROM role_mappings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DB exposes the connection so callers can run multi-store transactions.
func (s *UserStore) DB() *sql.DB { return s.db }

// HasUsers reports whether any account exists (false = first run).
func (s *UserStore) HasUsers() (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists)
	return exists, err
}

// CreateInitialAdmin creates the first account, as a local admin. It fails
// with ErrAlreadySetUp if any account already exists; the table lock makes
// two concurrent setup requests impossible to both succeed.
func (s *UserStore) CreateInitialAdmin(email, passwordHash, name string) (*auth.User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
		return nil, err
	}
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAlreadySetUp
	}

	user, err := insertLocalUser(tx, email, passwordHash, name, auth.RoleAdmin, false)
	if err != nil {
		return nil, err
	}
	return user, tx.Commit()
}

// ErrAlreadySetUp is returned by CreateInitialAdmin after the first run.
var ErrAlreadySetUp = errors.New("towercrane is already set up")

// CreateInvitedLocalUser creates a local account inside tx (used together
// with consuming the invitation, so both happen or neither does).
func (s *UserStore) CreateInvitedLocalUser(tx *sql.Tx, email, passwordHash, name string, role auth.Role) (*auth.User, error) {
	return insertLocalUser(tx, email, passwordHash, name, role, true)
}

func insertLocalUser(tx *sql.Tx, email, passwordHash, name string, role auth.Role, invited bool) (*auth.User, error) {
	u := &auth.User{}
	err := tx.QueryRow(`
	INSERT INTO users (id, email, name, password_hash, is_local_user, local_role, invited, created_at, updated_at)
	VALUES ($1, lower($2), $3, $4, TRUE, NULLIF($5, ''), $6, $7, $7)
	RETURNING id, github_id, github_login, email, name, avatar_url, created_at, updated_at`,
		uuid.New().String(), email, name, passwordHash, string(role), invited, time.Now()).
		Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// MarkInvited records, inside tx, that a user joined through an invitation
// and gives them the invited role (never lowering a role they already have).
func (s *UserStore) MarkInvited(tx *sql.Tx, userID string, role auth.Role) error {
	var current sql.NullString
	if err := tx.QueryRow(`SELECT local_role FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&current); err != nil {
		return err
	}
	newRole := auth.MaxRole(auth.Role(current.String), role)
	_, err := tx.Exec(`UPDATE users SET invited = TRUE, local_role = NULLIF($1, ''), updated_at = $2 WHERE id = $3`,
		string(newRole), time.Now(), userID)
	return err
}

// IsInvited reports whether the user joined through an invitation.
func (s *UserStore) IsInvited(userID string) (bool, error) {
	var invited bool
	err := s.db.QueryRow(`SELECT invited FROM users WHERE id = $1`, userID).Scan(&invited)
	return invited, err
}

// IsLocalUser reports whether the account signs in with email/password.
func (s *UserStore) IsLocalUser(userID string) (bool, error) {
	var local sql.NullBool
	err := s.db.QueryRow(`SELECT is_local_user FROM users WHERE id = $1`, userID).Scan(&local)
	return local.Bool, err
}

// IsAdminGitHubLogin reports whether a GitHub account belongs to a TowerCrane
// admin: listed in TOWERCRANE_ADMINS, or linked to a user whose effective
// role is admin.
func (s *UserStore) IsAdminGitHubLogin(login string) bool {
	if login == "" {
		return false
	}
	if auth.IsBootstrapAdmin(login, "") {
		return true
	}
	var id string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE lower(github_login) = lower($1)`, login).Scan(&id); err != nil {
		return false
	}
	sources, err := s.GetRoleSources(id)
	return err == nil && sources.Effective() == auth.RoleAdmin
}

// SessionsRevokedAt is when the user last signed out (zero if never): JWTs
// issued before it are no longer accepted.
func (s *UserStore) SessionsRevokedAt(userID string) (time.Time, error) {
	var t sql.NullTime
	if err := s.db.QueryRow(`SELECT sessions_revoked_at FROM users WHERE id = $1`, userID).Scan(&t); err != nil {
		return time.Time{}, err
	}
	return t.Time, nil
}

// RevokeSessions signs the user out everywhere: every JWT issued until now
// stops working. Stored with second precision, like a JWT's iat.
func (s *UserStore) RevokeSessions(userID string) error {
	_, err := s.db.Exec(`UPDATE users SET sessions_revoked_at = $2 WHERE id = $1`, userID, time.Now().UTC().Truncate(time.Second))
	return err
}
