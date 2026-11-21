// Package secrets encrypts credentials TowerCrane stores (e.g. cloud access
// keys) with AES-256-GCM.
//
// The key comes from TOWERCRANE_ENCRYPTION_KEY (32 bytes, base64). If it is
// unset, a key is derived from JWT_SECRET so development setups work, and a
// warning is logged: rotating JWT_SECRET would then make stored secrets
// unreadable, so production deployments should set a dedicated key.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

var (
	once    sync.Once
	aead    cipher.AEAD
	initErr error
)

func load() {
	var key []byte
	if raw := strings.TrimSpace(os.Getenv("TOWERCRANE_ENCRYPTION_KEY")); raw != "" {
		k, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(k) != 32 {
			initErr = errors.New("TOWERCRANE_ENCRYPTION_KEY must be 32 bytes, base64-encoded (openssl rand -base64 32)")
			return
		}
		key = k
	} else {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			initErr = errors.New("set TOWERCRANE_ENCRYPTION_KEY (or at least JWT_SECRET) to store secrets")
			return
		}
		log.Printf("WARNING: TOWERCRANE_ENCRYPTION_KEY is not set; deriving the secrets key from JWT_SECRET")
		sum := sha256.Sum256([]byte("towercrane-secrets:" + secret))
		key = sum[:]
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		initErr = err
		return
	}
	aead, initErr = cipher.NewGCM(block)
}

// Encrypt returns base64(nonce || ciphertext).
func Encrypt(plaintext string) (string, error) {
	once.Do(load)
	if initErr != nil {
		return "", initErr
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Decrypt reverses Encrypt.
func Decrypt(encoded string) (string, error) {
	once.Do(load)
	if initErr != nil {
		return "", initErr
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < aead.NonceSize() {
		return "", errors.New("malformed encrypted secret")
	}
	plain, err := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret (was the encryption key changed?): %w", err)
	}
	return string(plain), nil
}
