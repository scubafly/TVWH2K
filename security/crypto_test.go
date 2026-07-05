package security

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func newTestEncryptor(t *testing.T) *Encryptor {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	enc := newTestEncryptor(t)
	plaintext := "kraken-api-secret-123"

	data, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := enc.Decrypt(data)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("roundtrip mismatch: got %q want %q", got, plaintext)
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	enc := newTestEncryptor(t)
	a, _ := enc.Encrypt("same input")
	b, _ := enc.Encrypt("same input")
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext produced identical output (nonce reuse)")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	enc := newTestEncryptor(t)
	data, _ := enc.Encrypt("secret")
	data[len(data)-1] ^= 0xFF
	if _, err := enc.Decrypt(data); err == nil {
		t.Fatal("expected error for tampered ciphertext, got nil")
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	enc1 := newTestEncryptor(t)
	enc2 := newTestEncryptor(t)
	data, _ := enc1.Encrypt("secret")
	if _, err := enc2.Decrypt(data); err == nil {
		t.Fatal("expected error decrypting with a different key, got nil")
	}
}

func TestDecryptRejectsShortInput(t *testing.T) {
	enc := newTestEncryptor(t)
	if _, err := enc.Decrypt([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for too-short ciphertext, got nil")
	}
}

func TestNewEncryptorRejectsBadKeyLength(t *testing.T) {
	if _, err := NewEncryptor(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key, got nil")
	}
}
