// Package secrets encrypts credentials TowerCrane stores (e.g. cloud access
// keys) with AES-256-GCM.
//
// The key comes from TOWERCRANE_ENCRYPTION_KEY (32 bytes, base64), and is
// required: it is kept apart from JWT_SECRET so leaking one doesn't expose
// the other. Older versions derived the key from JWT_SECRET when it was
// unset; values encrypted that way can still be decrypted (legacy), and new
// values always use TOWERCRANE_ENCRYPTION_KEY.
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
	legacy  cipher.AEAD // key derived from JWT_SECRET by older versions
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
		initErr = errors.New("TOWERCRANE_ENCRYPTION_KEY is required (openssl rand -base64 32)")
		return
	}

	aead, initErr = newAEAD(key)
	if initErr != nil {
		return
	}
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		sum := sha256.Sum256([]byte("towercrane-secrets:" + secret))
		legacy, _ = newAEAD(sum[:])
	}
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Check reports whether secrets can be encrypted (the key is set and valid).
func Check() error {
	once.Do(load)
	return initErr
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
	if err != nil && legacy != nil {
		if p, lerr := legacy.Open(nil, data[:legacy.NonceSize()], data[legacy.NonceSize():], nil); lerr == nil {
			log.Printf("secrets: decrypted a value stored with the old JWT_SECRET-derived key; save it again to re-encrypt it")
			plain, err = p, nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret (was the encryption key changed?): %w", err)
	}
	return string(plain), nil
}
