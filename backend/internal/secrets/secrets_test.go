package secrets

import (
	"strings"
	"sync"
	"testing"
)

func reset() { once = sync.Once{}; aead = nil; initErr = nil }

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
