package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func testKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestFromEnvReadsKeyFromVariable(t *testing.T) {
	// One line with literal "\n", as stored in .env files and secret stores.
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", strings.ReplaceAll(testKeyPEM(t), "\n", `\n`))
	t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", "")
	app, err := FromEnv()
	if err != nil || app == nil {
		t.Fatalf("FromEnv = %v, %v", app, err)
	}
	if _, err := app.appJWT(); err != nil {
		t.Errorf("signing the app JWT failed: %v", err)
	}
}

func TestFromEnvNotConfiguredAndInvalid(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", "")
	if app, err := FromEnv(); app != nil || err != nil {
		t.Errorf("unconfigured app must be (nil, nil), got %v, %v", app, err)
	}
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not a key")
	if _, err := FromEnv(); err == nil {
		t.Error("an invalid key must be an error")
	}
}
