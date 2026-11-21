package secrets

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
)

func reset() { once = sync.Once{}; aead = nil; legacy = nil; initErr = nil }

func TestRoundTrip(t *testing.T) {
	reset()
	t.Setenv("TOWERCRANE_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	enc, err := Encrypt("wJalrXUtnFEMI/K7MDENG")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "wJalr") {
		t.Fatal("ciphertext leaks plaintext")
	}
	enc2, _ := Encrypt("wJalrXUtnFEMI/K7MDENG")
	if enc == enc2 {
		t.Error("two encryptions of the same value must differ (random nonce)")
	}
	dec, err := Decrypt(enc)
	if err != nil || dec != "wJalrXUtnFEMI/K7MDENG" {
		t.Fatalf("Decrypt = %q, %v", dec, err)
	}
	tampered := []byte(enc)
	tampered[len(tampered)-3] ^= 1
	if _, err := Decrypt(string(tampered)); err == nil {
		t.Error("tampered ciphertext must not decrypt")
	}
}

func TestBadKeyRejected(t *testing.T) {
	reset()
	t.Setenv("TOWERCRANE_ENCRYPTION_KEY", "c2hvcnQ=")
	if _, err := Encrypt("x"); err == nil {
		t.Error("a key that isn't 32 bytes must be rejected")
	}
}

func TestKeyRequired(t *testing.T) {
	reset()
	t.Setenv("TOWERCRANE_ENCRYPTION_KEY", "")
	t.Setenv("JWT_SECRET", "a-long-enough-jwt-secret-for-the-test-only")
	if Check() == nil {
		t.Error("JWT_SECRET alone must not be enough")
	}
}

func TestLegacyValuesStillDecrypt(t *testing.T) {
	reset()
	const jwt = "a-long-enough-jwt-secret-for-the-test-only"
	t.Setenv("JWT_SECRET", jwt)
	sum := sha256.Sum256([]byte("towercrane-secrets:" + jwt))
	old, _ := newAEAD(sum[:])
	nonce := make([]byte, old.NonceSize())
	enc := base64.StdEncoding.EncodeToString(old.Seal(nonce, nonce, []byte("old-value"), nil))

	t.Setenv("TOWERCRANE_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if got, err := Decrypt(enc); err != nil || got != "old-value" {
		t.Fatalf("Decrypt(legacy) = %q, %v", got, err)
	}
}
