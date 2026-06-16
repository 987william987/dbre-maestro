package crypto_test

import (
	"bytes"
	"testing"

	"github.com/dbre-maestro/maestro/internal/crypto"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("super-secret-password-123")

	ct, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	got, err := crypto.Decrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("same plaintext")

	ct1, _ := crypto.Encrypt(key, plaintext)
	ct2, _ := crypto.Encrypt(key, plaintext)
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of the same plaintext should differ (random nonce)")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 0xFF

	ct, _ := crypto.Encrypt(key1, []byte("secret"))
	_, err := crypto.Decrypt(key2, ct)
	if err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestDecryptTruncated(t *testing.T) {
	key := make([]byte, 32)
	_, err := crypto.Decrypt(key, []byte("short"))
	if err == nil {
		t.Error("expected error decrypting truncated ciphertext")
	}
}
