package export

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateToken creates a 32-byte crypto/rand token encoded as hex (64 chars).
// Stored for legacy download links; new UI uses authenticated export IDs.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate export token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
