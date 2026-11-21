package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// IntegrationStore holds GitHub App installations, user invitations, cloud
// accounts and the PR policy checks TowerCrane reports to GitHub.
type IntegrationStore struct {
	db *sql.DB
}

func NewIntegrationStore(db *sql.DB) *IntegrationStore {
	return &IntegrationStore{db: db}
}

func (s *IntegrationStore) CreateTables() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS github_installations (
		installation_id BIGINT PRIMARY KEY,
		account_login VARCHAR(255) NOT NULL,
		account_type VARCHAR(32) NOT NULL,
		repository_selection VARCHAR(32) NOT NULL,
		added_by UUID NOT NULL,
		created_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS invitations (
		id UUID PRIMARY KEY,
		email VARCHAR(255) NOT NULL,
		role VARCHAR(20) NOT NULL,
		token_hash VARCHAR(64) UNIQUE NOT NULL,
		invited_by UUID NOT NULL,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		accepted_by UUID,
		accepted_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS cloud_accounts (
		id UUID PRIMARY KEY,
		name VARCHAR(255) UNIQUE NOT NULL,
		provider VARCHAR(20) NOT NULL,
		aws_account_id VARCHAR(12) NOT NULL,
		access_key_id VARCHAR(128) NOT NULL,
		secret_enc TEXT NOT NULL,
		region VARCHAR(32) NOT NULL,
		endpoint_url VARCHAR(512),
		created_by UUID NOT NULL,
		created_at TIMESTAMP NOT NULL,
		verified_at TIMESTAMP,
		verify_error TEXT
	);

	CREATE TABLE IF NOT EXISTS pr_checks (
		repository_id UUID NOT NULL,
		pr_number INTEGER NOT NULL,
		head_sha VARCHAR(40) NOT NULL,
		state VARCHAR(20) NOT NULL,
		description TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL,
		PRIMARY KEY (repository_id, pr_number)
	);

	-- Cloud accounts a repository may use (manifests name them).
	CREATE TABLE IF NOT EXISTS repository_cloud_accounts (
		repository_id UUID NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
		cloud_account_id UUID NOT NULL REFERENCES cloud_accounts(id) ON DELETE CASCADE,
		PRIMARY KEY (repository_id, cloud_account_id)
	);
	`)
	if err != nil {
		return err
	}
	// Repositories used to have a single cloud account: attach it once.
	_, err = s.db.Exec(`
		INSERT INTO repository_cloud_accounts (repository_id, cloud_account_id)
		SELECT r.id, r.cloud_account_id FROM repositories r
		JOIN cloud_accounts a ON a.id = r.cloud_account_id
		ON CONFLICT DO NOTHING;
		UPDATE repositories SET cloud_account_id = NULL WHERE cloud_account_id IS NOT NULL;`)
	return err
}

// --- GitHub App installations -----------------------------------------------

type Installation struct {
	InstallationID      int64     `json:"installation_id"`
	AccountLogin        string    `json:"account_login"`
	AccountType         string    `json:"account_type"`         // Organization or User
	RepositorySelection string    `json:"repository_selection"` // all or selected
	AddedBy             string    `json:"added_by"`
	CreatedAt           time.Time `json:"created_at"`
}

func (s *IntegrationStore) UpsertInstallation(i *Installation) error {
	_, err := s.db.Exec(`
	INSERT INTO github_installations (installation_id, account_login, account_type, repository_selection, added_by, created_at)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (installation_id) DO UPDATE SET account_login = EXCLUDED.account_login,
		account_type = EXCLUDED.account_type, repository_selection = EXCLUDED.repository_selection`,
		i.InstallationID, i.AccountLogin, i.AccountType, i.RepositorySelection, i.AddedBy, time.Now())
	return err
}

func (s *IntegrationStore) ListInstallations() ([]Installation, error) {
	rows, err := s.db.Query(`SELECT installation_id, account_login, account_type, repository_selection, added_by, created_at
		FROM github_installations ORDER BY account_login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Installation{}
	for rows.Next() {
		var i Installation
		if err := rows.Scan(&i.InstallationID, &i.AccountLogin, &i.AccountType, &i.RepositorySelection, &i.AddedBy, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *IntegrationStore) DeleteInstallation(id int64) error {
	_, err := s.db.Exec(`DELETE FROM github_installations WHERE installation_id = $1`, id)
	return err
}

// --- Invitations ------------------------------------------------------------

type Invitation struct {
	ID         string     `json:"id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	InvitedBy  string     `json:"invited_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedBy string     `json:"accepted_by,omitempty"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

// HashToken is how invitation tokens are stored: only the hash, never the
// token itself, so a database leak can't be used to accept invitations.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *IntegrationStore) CreateInvitation(email, role, token, invitedBy string, ttl time.Duration) (*Invitation, error) {
	inv := &Invitation{
		ID: uuid.New().String(), Email: email, Role: role, InvitedBy: invitedBy,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(ttl),
	}
	_, err := s.db.Exec(`INSERT INTO invitations (id, email, role, token_hash, invited_by, created_at, expires_at)
		VALUES ($1, lower($2), $3, $4, $5, $6, $7)`,
		inv.ID, inv.Email, inv.Role, HashToken(token), inv.InvitedBy, inv.CreatedAt, inv.ExpiresAt)
	return inv, err
}

// GetOpenInvitation returns a not-yet-accepted, unexpired invitation.
func (s *IntegrationStore) GetOpenInvitation(token string) (*Invitation, error) {
	inv := &Invitation{}
	err := s.db.QueryRow(`SELECT id, email, role, invited_by, created_at, expires_at FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > $2`, HashToken(token), time.Now()).
		Scan(&inv.ID, &inv.Email, &inv.Role, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// AcceptInvitation marks an invitation as used, exactly once.
func (s *IntegrationStore) AcceptInvitation(tx *sql.Tx, id, userID string) error {
	res, err := tx.Exec(`UPDATE invitations SET accepted_by = $1, accepted_at = $2
		WHERE id = $3 AND accepted_at IS NULL AND expires_at > $2`, userID, time.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *IntegrationStore) ListInvitations() ([]Invitation, error) {
	rows, err := s.db.Query(`SELECT id, email, role, invited_by, created_at, expires_at, accepted_by, accepted_at
		FROM invitations ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		var acceptedBy sql.NullString
		var acceptedAt sql.NullTime
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.Role, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt, &acceptedBy, &acceptedAt); err != nil {
			return nil, err
		}
		inv.AcceptedBy = acceptedBy.String
		if acceptedAt.Valid {
			inv.AcceptedAt = &acceptedAt.Time
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvitation deletes an invitation that hasn't been accepted yet.
func (s *IntegrationStore) RevokeInvitation(id string) error {
	res, err := s.db.Exec(`DELETE FROM invitations WHERE id = $1 AND accepted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- Cloud accounts -----------------------------------------------------------

// CloudAccount holds credentials TowerCrane injects into tofu. SecretEnc is
// encrypted at rest and never leaves the backend.
type CloudAccount struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Provider     string     `json:"provider"`
	AWSAccountID string     `json:"aws_account_id"`
	AccessKeyID  string     `json:"-"`
	SecretEnc    string     `json:"-"`
	Region       string     `json:"region"`
	EndpointURL  string     `json:"endpoint_url,omitempty"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	VerifyError  string     `json:"verify_error,omitempty"`
}

const cloudAccountColumns = `id, name, provider, aws_account_id, access_key_id, secret_enc, region,
	COALESCE(endpoint_url, ''), created_by, created_at, verified_at, COALESCE(verify_error, '')`

func scanCloudAccount(row interface{ Scan(...any) error }) (*CloudAccount, error) {
	a := &CloudAccount{}
	var verifiedAt sql.NullTime
	if err := row.Scan(&a.ID, &a.Name, &a.Provider, &a.AWSAccountID, &a.AccessKeyID, &a.SecretEnc, &a.Region,
		&a.EndpointURL, &a.CreatedBy, &a.CreatedAt, &verifiedAt, &a.VerifyError); err != nil {
		return nil, err
	}
	if verifiedAt.Valid {
		a.VerifiedAt = &verifiedAt.Time
	}
	return a, nil
}

func (s *IntegrationStore) CreateCloudAccount(a *CloudAccount) error {
	a.ID = uuid.New().String()
	a.CreatedAt = time.Now()
	_, err := s.db.Exec(`INSERT INTO cloud_accounts (id, name, provider, aws_account_id, access_key_id, secret_enc,
		region, endpoint_url, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10)`,
		a.ID, a.Name, a.Provider, a.AWSAccountID, a.AccessKeyID, a.SecretEnc, a.Region, a.EndpointURL, a.CreatedBy, a.CreatedAt)
	return err
}

func (s *IntegrationStore) GetCloudAccount(id string) (*CloudAccount, error) {
	return scanCloudAccount(s.db.QueryRow(`SELECT `+cloudAccountColumns+` FROM cloud_accounts WHERE id = $1`, id))
}

func (s *IntegrationStore) ListCloudAccounts() ([]*CloudAccount, error) {
	rows, err := s.db.Query(`SELECT ` + cloudAccountColumns + ` FROM cloud_accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CloudAccount{}
	for rows.Next() {
		a, err := scanCloudAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *IntegrationStore) SetCloudAccountVerification(id string, verifyErr string) error {
	var err error
	if verifyErr == "" {
		_, err = s.db.Exec(`UPDATE cloud_accounts SET verified_at = $1, verify_error = NULL WHERE id = $2`, time.Now(), id)
	} else {
		_, err = s.db.Exec(`UPDATE cloud_accounts SET verified_at = NULL, verify_error = $1 WHERE id = $2`, verifyErr, id)
	}
	return err
}

// DeleteCloudAccount removes an account (and detaches it from repositories).
func (s *IntegrationStore) DeleteCloudAccount(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM cloud_accounts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// --- PR policy checks ---------------------------------------------------------

type PRCheck struct {
	RepositoryID string    `json:"repository_id"`
	PRNumber     int       `json:"pr_number"`
	HeadSHA      string    `json:"head_sha"`
	State        string    `json:"state"`
	Description  string    `json:"description"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *IntegrationStore) UpsertPRCheck(c *PRCheck) error {
	_, err := s.db.Exec(`
	INSERT INTO pr_checks (repository_id, pr_number, head_sha, state, description, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (repository_id, pr_number) DO UPDATE SET head_sha = EXCLUDED.head_sha,
		state = EXCLUDED.state, description = EXCLUDED.description, updated_at = EXCLUDED.updated_at`,
		c.RepositoryID, c.PRNumber, c.HeadSHA, c.State, c.Description, time.Now())
	return err
}

func (s *IntegrationStore) ListPRChecks(repoID string) ([]PRCheck, error) {
	rows, err := s.db.Query(`SELECT repository_id, pr_number, head_sha, state, description, updated_at
		FROM pr_checks WHERE repository_id = $1 ORDER BY pr_number DESC`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PRCheck{}
	for rows.Next() {
		var c PRCheck
		if err := rows.Scan(&c.RepositoryID, &c.PRNumber, &c.HeadSHA, &c.State, &c.Description, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *IntegrationStore) DeletePRCheck(repoID string, prNumber int) error {
	_, err := s.db.Exec(`DELETE FROM pr_checks WHERE repository_id = $1 AND pr_number = $2`, repoID, prNumber)
	return err
}

// GetCloudAccountByName finds a cloud account by the name manifests use.
func (s *IntegrationStore) GetCloudAccountByName(name string) (*CloudAccount, error) {
	return scanCloudAccount(s.db.QueryRow(`SELECT `+cloudAccountColumns+` FROM cloud_accounts WHERE name = $1`, name))
}

// RepositoryCloudAccounts lists the cloud accounts attached to a repository.
func (s *IntegrationStore) RepositoryCloudAccounts(repoID string) ([]*CloudAccount, error) {
	rows, err := s.db.Query(`SELECT `+cloudAccountColumns+` FROM cloud_accounts
		WHERE id IN (SELECT cloud_account_id FROM repository_cloud_accounts WHERE repository_id = $1)
		ORDER BY name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*CloudAccount{}
	for rows.Next() {
		a, err := scanCloudAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RepositoryCloudAccountIDs maps every repository to its attached cloud
// account ids.
func (s *IntegrationStore) RepositoryCloudAccountIDs() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT repository_id, cloud_account_id FROM repository_cloud_accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var repo, acct string
		if err := rows.Scan(&repo, &acct); err != nil {
			return nil, err
		}
		out[repo] = append(out[repo], acct)
	}
	return out, rows.Err()
}

// SetRepositoryCloudAccounts replaces the cloud accounts attached to a
// repository.
func (s *IntegrationStore) SetRepositoryCloudAccounts(repoID string, accountIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM repository_cloud_accounts WHERE repository_id = $1`, repoID); err != nil {
		return err
	}
	for _, id := range accountIDs {
		if _, err := tx.Exec(`INSERT INTO repository_cloud_accounts (repository_id, cloud_account_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, repoID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
