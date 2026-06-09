package masking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// MaskMode defines how a sensitive column is masked.
type MaskMode string

const (
	MaskModeFull    MaskMode = "full"    // "****"
	MaskModePartial MaskMode = "partial" // "138****8888"
	MaskModeHash    MaskMode = "hash"    // HMAC-SHA256 hex
)

// Rule describes how one column should be masked.
type Rule struct {
	Table  string   // table name (case-insensitive)
	Column string   // column name (case-insensitive)
	Mode   MaskMode
}

// TE5: Apply masks a string value according to the rule's mode.
// pepper is derived from DBRE_ENCRYPTION_KEY via HKDF so dictionary attacks are ineffective.
func (r Rule) Apply(value string, pepper []byte) (string, error) {
	switch r.Mode {
	case MaskModeFull:
		return "****", nil
	case MaskModePartial:
		return partial(value), nil
	case MaskModeHash:
		return hmacHash(value, pepper), nil
	default:
		return "", fmt.Errorf("unknown mask mode: %s", r.Mode)
	}
}

// DeriveHashPepper derives a 32-byte pepper from the platform encryption key using HKDF-SHA256.
// Different pepper per deployment makes pre-computed rainbow tables useless.
func DeriveHashPepper(encryptionKey []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, encryptionKey, nil, []byte("dbre-maestro-masking-pepper-v1"))
	pepper := make([]byte, 32)
	if _, err := r.Read(pepper); err != nil {
		return nil, fmt.Errorf("derive pepper: %w", err)
	}
	return pepper, nil
}

func hmacHash(value string, pepper []byte) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func partial(value string) string {
	n := len([]rune(value))
	switch {
	case n <= 2:
		return strings.Repeat("*", n)
	case n <= 6:
		return string([]rune(value)[:1]) + strings.Repeat("*", n-2) + string([]rune(value)[n-1:])
	default:
		// Keep first 3 and last 4 characters
		runes := []rune(value)
		keep := 3
		tail := 4
		if n < keep+tail+1 {
			keep = 1
			tail = 1
		}
		return string(runes[:keep]) + strings.Repeat("*", n-keep-tail) + string(runes[n-tail:])
	}
}
