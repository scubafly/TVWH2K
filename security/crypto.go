// Package security provides at-rest encryption for sensitive per-connection
// secrets (Kraken API keys, Telegram bot tokens) stored in Postgres.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

// Encryptor encrypts/decrypts secrets with AES-256-GCM using a single
// master key loaded once at startup from ENCRYPTION_KEY.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptorFromEnv loads ENCRYPTION_KEY (a base64-encoded 32-byte key)
// and builds an Encryptor. Generate a key with:
//
//	openssl rand -base64 32
func NewEncryptorFromEnv() (*Encryptor, error) {
	keyB64 := os.Getenv("ENCRYPTION_KEY")
	if keyB64 == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY not set")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY is not valid base64: %w", err)
	}
	return NewEncryptor(key)
}

// NewEncryptor builds an Encryptor from a raw 32-byte AES-256 key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (AES-256), got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to init AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to init GCM: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

// Encrypt returns nonce||ciphertext, ready to store in a BYTEA column.
func (e *Encryptor) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return e.gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt.
func (e *Encryptor) Decrypt(data []byte) (string, error) {
	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}
	return string(plaintext), nil
}
