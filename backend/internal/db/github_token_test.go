package db

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Needs a Postgres: TOWERCRANE_TEST_DB=1 with DB_* pointing at a test database.
func TestGitHubTokenEncryptedAtRest(t *testing.T) {
	if os.Getenv("TOWERCRANE_TEST_DB") == "" {
		t.Skip("set TOWERCRANE_TEST_DB=1 and DB_* to run against a test database")
	}
	t.Setenv("TOWERCRANE_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=") // 32 bytes, base64
	database, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := NewUserStore(database)
	if err := store.CreateTables(); err != nil {
		t.Fatal(err)
	}
	id := uuid.New().String()
	if _, err := database.Exec(`INSERT INTO users (id, email, name, created_at, updated_at, github_access_token)
		VALUES ($1, $2, 'tok-test', $3, $3, 'gho_legacy_plain')`, id, id+"@test.invalid", time.Now()); err != nil {
		t.Fatal(err)
	}
	defer database.Exec(`DELETE FROM users WHERE id = $1`, id)

	if n, err := store.EncryptGitHubTokens(); err != nil || n < 1 {
		t.Fatalf("migration: n=%d err=%v", n, err)
	}
	var raw string
	database.QueryRow(`SELECT github_access_token FROM users WHERE id = $1`, id).Scan(&raw)
	if !strings.HasPrefix(raw, encryptedTokenPrefix) || strings.Contains(raw, "gho_legacy_plain") {
		t.Fatalf("token still readable in the database: %q", raw)
	}
	if got, err := store.GetGitHubToken(id); err != nil || got != "gho_legacy_plain" {
		t.Fatalf("GetGitHubToken = %q, %v", got, err)
	}
}
