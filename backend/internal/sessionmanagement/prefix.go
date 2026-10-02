package sessionmanagement

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	PreviewTTL              = 60 * time.Second
	MinimumPrefixCharacters = 16
	MaximumPrefixCharacters = 4096
	maximumActivePreviews   = 1000
)

var (
	ErrPreviewNotFound = errors.New("prefix preview token not found or expired")
	ErrPreviewMismatch = errors.New("prefix preview token does not match request")
)

type PrefixPreview struct {
	ActorID           uint64
	ConnectionID      uint64
	TargetKey         string
	Engine            string
	Database          string
	NormalizedPrefix  string
	MinimumAgeSeconds float64
	Matches           []Session
	ExpiresAt         time.Time
}

type PreviewStore struct {
	mu      sync.Mutex
	entries map[string]PrefixPreview
	now     func() time.Time
}

func NewPreviewStore() *PreviewStore {
	return &PreviewStore{entries: make(map[string]PrefixPreview), now: time.Now}
}

func (s *PreviewStore) Create(preview PrefixPreview) (string, PrefixPreview, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", PrefixPreview{}, fmt.Errorf("generate preview token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	preview.ExpiresAt = s.now().Add(PreviewTTL)
	preview.Matches = append([]Session(nil), preview.Matches...)
	for i := range preview.Matches {
		preview.Matches[i].Query = ""
		preview.Matches[i].Command = ""
	}
	s.mu.Lock()
	s.removeExpiredLocked()
	for existingToken, existing := range s.entries {
		if existing.ActorID == preview.ActorID && existing.ConnectionID == preview.ConnectionID && existing.TargetKey == preview.TargetKey {
			delete(s.entries, existingToken)
		}
	}
	if len(s.entries) >= maximumActivePreviews {
		var oldestToken string
		var oldestExpiry time.Time
		for existingToken, existing := range s.entries {
			if oldestToken == "" || existing.ExpiresAt.Before(oldestExpiry) {
				oldestToken, oldestExpiry = existingToken, existing.ExpiresAt
			}
		}
		delete(s.entries, oldestToken)
	}
	s.entries[token] = preview
	s.mu.Unlock()
	return token, preview, nil
}

func (s *PreviewStore) Consume(token string, actorID, connectionID uint64, targetKey string) (PrefixPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeExpiredLocked()
	preview, ok := s.entries[token]
	if !ok {
		return PrefixPreview{}, ErrPreviewNotFound
	}
	if preview.ActorID != actorID || preview.ConnectionID != connectionID || preview.TargetKey != targetKey {
		return PrefixPreview{}, ErrPreviewMismatch
	}
	delete(s.entries, token)
	return preview, nil
}

func (s *PreviewStore) removeExpiredLocked() {
	now := s.now()
	for token, preview := range s.entries {
		if !now.Before(preview.ExpiresAt) {
			delete(s.entries, token)
		}
	}
}

func NormalizePrefix(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func ValidatePrefix(value string) (string, error) {
	normalized := NormalizePrefix(value)
	characters := 0
	for _, r := range normalized {
		if !unicode.IsSpace(r) {
			characters++
		}
	}
	if characters < MinimumPrefixCharacters {
		return "", fmt.Errorf("prefix must contain at least %d non-whitespace characters", MinimumPrefixCharacters)
	}
	if utf8.RuneCountInString(normalized) > MaximumPrefixCharacters {
		return "", fmt.Errorf("prefix must contain at most %d characters", MaximumPrefixCharacters)
	}
	return normalized, nil
}

func MatchPrefixSessions(items []Session, database, normalizedPrefix string, minimumAgeSeconds float64, engine string) []Session {
	matches := make([]Session, 0)
	for _, item := range items {
		if item.Protected || item.Database != database || item.DurationSeconds < minimumAgeSeconds || !sessionIsActive(item, engine) {
			continue
		}
		if strings.HasPrefix(NormalizePrefix(item.Query), normalizedPrefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

func sessionIsActive(item Session, engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql":
		return strings.EqualFold(item.Command, "query") && strings.TrimSpace(item.Query) != ""
	case "postgres", "postgresql":
		return strings.EqualFold(item.State, "active") && strings.TrimSpace(item.Query) != ""
	default:
		return false
	}
}

func PrefixHash(normalizedPrefix string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalizedPrefix)))
}

// SanitizeSQLShape removes literals and comments without requiring the input
// to be a complete SQL statement, which is important for prefix matching.
func SanitizeSQLShape(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		switch {
		case i+1 < len(value) && value[i] == '-' && value[i+1] == '-':
			i += 2
			for i < len(value) && value[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
		case value[i] == '#':
			for i < len(value) && value[i] != '\n' {
				i++
			}
			out.WriteByte(' ')
		case i+1 < len(value) && value[i] == '/' && value[i+1] == '*':
			i += 2
			for i+1 < len(value) && !(value[i] == '*' && value[i+1] == '/') {
				i++
			}
			if i+1 < len(value) {
				i += 2
			}
			out.WriteByte(' ')
		case value[i] == '\'' || value[i] == '"':
			quote := value[i]
			i++
			for i < len(value) {
				if value[i] == '\\' {
					i += 2
					continue
				}
				if value[i] == quote {
					if i+1 < len(value) && value[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			out.WriteByte('?')
		case value[i] == '$':
			if end, ok := dollarQuoteEnd(value, i); ok {
				tag := value[i:end]
				i = end
				if closeAt := strings.Index(value[i:], tag); closeAt >= 0 {
					i += closeAt + len(tag)
				} else {
					i = len(value)
				}
				out.WriteByte('?')
			} else {
				out.WriteByte(value[i])
				i++
			}
		case isNumberStart(value, i):
			i = scanNumber(value, i)
			out.WriteByte('?')
		default:
			out.WriteByte(value[i])
			i++
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func dollarQuoteEnd(value string, start int) (int, bool) {
	for i := start + 1; i < len(value); i++ {
		if value[i] == '$' {
			return i + 1, true
		}
		if !(value[i] == '_' || value[i] >= 'a' && value[i] <= 'z' || value[i] >= 'A' && value[i] <= 'Z' || value[i] >= '0' && value[i] <= '9') {
			return 0, false
		}
	}
	return 0, false
}

func isNumberStart(value string, i int) bool {
	if value[i] < '0' || value[i] > '9' {
		return false
	}
	return i == 0 || !(value[i-1] == '_' || value[i-1] >= 'a' && value[i-1] <= 'z' || value[i-1] >= 'A' && value[i-1] <= 'Z' || value[i-1] >= '0' && value[i-1] <= '9')
}

func scanNumber(value string, start int) int {
	i := start
	if i+1 < len(value) && value[i] == '0' && (value[i+1] == 'x' || value[i+1] == 'X') {
		i += 2
		for i < len(value) && (value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f' || value[i] >= 'A' && value[i] <= 'F') {
			i++
		}
		return i
	}
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i < len(value) && value[i] == '.' {
		i++
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
	}
	if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
		exponent := i
		i++
		if i < len(value) && (value[i] == '+' || value[i] == '-') {
			i++
		}
		digits := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if digits == i {
			return exponent
		}
	}
	return i
}
