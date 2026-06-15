package masking

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

// MaskMode defines how a sensitive column is masked.
type MaskMode string

const (
	MaskModeFull     MaskMode = "full"
	MaskModePartial  MaskMode = "partial"
	MaskModeHash     MaskMode = "hash"
	MaskModeEmail    MaskMode = "email"
	MaskModeFixed    MaskMode = "fixed"
	MaskModeNumeric  MaskMode = "numeric"
	MaskModeDateTime MaskMode = "datetime"
	MaskModeIP       MaskMode = "ip"
)

type MatchType string

const (
	MatchTypeExact MatchType = "exact"
	MatchTypeRegex MatchType = "regex"
)

// Rule describes how one column should be masked.
type Rule struct {
	Database string   // database name (case-insensitive)
	Schema   string   // schema name (case-insensitive)
	Table    string   // table name (case-insensitive)
	Column   string   // column name or regex pattern
	Match    MatchType
	Mode     MaskMode
	Config   json.RawMessage
}

// TE5: Apply masks a string value according to the rule's mode.
// pepper is derived from DBRE_ENCRYPTION_KEY via HKDF so dictionary attacks are ineffective.
func (r Rule) Apply(value string, pepper []byte) (string, error) {
	switch r.Mode {
	case MaskModeFull:
		return "****", nil
	case MaskModePartial:
		return partialWithConfig(value, r.Config)
	case MaskModeHash:
		return hmacHash(value, pepper), nil
	case MaskModeEmail:
		return maskEmail(value, r.Config)
	case MaskModeFixed:
		return fixedMask(r.Config)
	case MaskModeNumeric:
		return maskAmount(value, r.Config)
	case MaskModeDateTime:
		return truncateDateTime(value, r.Config)
	case MaskModeIP:
		return maskIP(value, r.Config)
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

type partialConfig struct {
	KeepPrefix      int    `json:"keep_prefix"`
	KeepSuffix      int    `json:"keep_suffix"`
	MaskChar        string `json:"mask_char"`
	MaskText        string `json:"mask_text"`
	FixedMaskLength int    `json:"fixed_mask_length"`
}

func partialWithConfig(value string, raw json.RawMessage) (string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return partial(value), nil
	}

	var cfg partialConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parse partial config: %w", err)
	}

	runes := []rune(strings.TrimSpace(value))
	n := len(runes)
	if n == 0 {
		return "", nil
	}

	if cfg.KeepPrefix < 0 || cfg.KeepSuffix < 0 || cfg.FixedMaskLength < 0 {
		return "", fmt.Errorf("partial config values must be non-negative")
	}
	if cfg.KeepPrefix+cfg.KeepSuffix > n {
		return partial(value), nil
	}

	maskText := cfg.MaskText
	if maskText == "" {
		maskChar := cfg.MaskChar
		if maskChar == "" {
			maskChar = "*"
		}
		maskLen := n - cfg.KeepPrefix - cfg.KeepSuffix
		if cfg.FixedMaskLength > 0 {
			maskLen = cfg.FixedMaskLength
		}
		maskText = strings.Repeat(maskChar, maxInt(maskLen, 0))
	}

	return string(runes[:cfg.KeepPrefix]) + maskText + string(runes[n-cfg.KeepSuffix:]), nil
}

type emailConfig struct {
	KeepLocalPrefix int    `json:"keep_local_prefix"`
	KeepDomain      bool   `json:"keep_domain"`
	Replacement     string `json:"replacement"`
}

func maskEmail(value string, raw json.RawMessage) (string, error) {
	var cfg emailConfig
	cfg.KeepLocalPrefix = 1
	cfg.KeepDomain = true
	cfg.Replacement = "****"
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("parse email config: %w", err)
		}
	}

	parts := strings.Split(strings.TrimSpace(value), "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return partial(value), nil
	}
	local := []rune(parts[0])
	keep := minInt(maxInt(cfg.KeepLocalPrefix, 0), len(local))
	domain := ""
	if cfg.KeepDomain {
		domain = "@" + parts[1]
	}
	return string(local[:keep]) + cfg.Replacement + domain, nil
}

type fixedConfig struct {
	Value string `json:"value"`
}

func fixedMask(raw json.RawMessage) (string, error) {
	var cfg fixedConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", fmt.Errorf("parse fixed config: %w", err)
	}
	if strings.TrimSpace(cfg.Value) == "" {
		return "", fmt.Errorf("fixed config value is required")
	}
	return cfg.Value, nil
}

type numericConfig struct {
	Operation string `json:"operation"`
	Decimals  int    `json:"decimals"`
}

func maskAmount(value string, raw json.RawMessage) (string, error) {
	cfg := numericConfig{Operation: "round", Decimals: 0}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("parse numeric config: %w", err)
		}
	}

	if cfg.Operation == "zero" {
		return "0", nil
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	number, ok := new(big.Float).SetString(trimmed)
	if !ok {
		return partial(value), nil
	}

	if cfg.Decimals <= 0 {
		intPart, _ := number.Int(nil)
		return intPart.String(), nil
	}

	floatValue, _ := number.Float64()
	pow := math.Pow10(cfg.Decimals)
	rounded := math.Round(floatValue*pow) / pow
	return fmt.Sprintf("%.*f", cfg.Decimals, rounded), nil
}

type dateTimeConfig struct {
	Granularity string `json:"granularity"`
}

func truncateDateTime(value string, raw json.RawMessage) (string, error) {
	cfg := dateTimeConfig{Granularity: "day"}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("parse datetime config: %w", err)
		}
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.999999",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			if cfg.Granularity == "hour" {
				return parsed.Format("2006-01-02 15:00:00"), nil
			}
			return parsed.Format("2006-01-02"), nil
		}
	}

	if len(trimmed) >= 13 && cfg.Granularity == "hour" {
		return strings.ReplaceAll(trimmed[:13], "T", " ") + ":00:00", nil
	}
	if len(trimmed) >= 10 {
		return trimmed[:10], nil
	}
	return partial(value), nil
}

type ipConfig struct {
	KeepSegments int `json:"keep_segments"`
}

func maskIP(value string, raw json.RawMessage) (string, error) {
	cfg := ipConfig{KeepSegments: 2}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("parse ip config: %w", err)
		}
	}

	trimmed := strings.TrimSpace(value)
	ip := net.ParseIP(trimmed)
	if ip == nil {
		return partial(value), nil
	}

	if ipv4 := ip.To4(); ipv4 != nil {
		segments := []string{
			fmt.Sprintf("%d", ipv4[0]),
			fmt.Sprintf("%d", ipv4[1]),
			fmt.Sprintf("%d", ipv4[2]),
			fmt.Sprintf("%d", ipv4[3]),
		}
		for i := maxInt(cfg.KeepSegments, 0); i < len(segments); i++ {
			segments[i] = "***"
		}
		return strings.Join(segments, "."), nil
	}

	parts := strings.Split(trimmed, ":")
	for i := maxInt(cfg.KeepSegments, 0); i < len(parts); i++ {
		parts[i] = "****"
	}
	return strings.Join(parts, ":"), nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func MatchColumnPattern(rule Rule, columnName string) (bool, error) {
	switch rule.Match {
	case "", MatchTypeExact:
		return equalFold(rule.Column, columnName), nil
	case MatchTypeRegex:
		pattern := strings.TrimSpace(rule.Column)
		if pattern == "" {
			return false, nil
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			return false, fmt.Errorf("compile rule regex %q: %w", rule.Column, err)
		}
		return re.MatchString(strings.TrimSpace(columnName)), nil
	default:
		return false, fmt.Errorf("unknown match type: %s", rule.Match)
	}
}
