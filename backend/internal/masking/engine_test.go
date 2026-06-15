package masking_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dbre-maestro/maestro/internal/masking"
)

var testKey = make([]byte, 32) // all-zeros key, fine for tests

func newEngine(t *testing.T) *masking.Engine {
	t.Helper()
	e, err := masking.NewEngine(testKey, masking.GlobalCache())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestMaskFull(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns: []string{"phone"},
		Origins: []masking.ColumnOrigin{{Table: "users", Column: "phone"}},
		Rows:    [][]any{{"13812345678"}},
	}
	rules := []masking.Rule{{Table: "users", Column: "phone", Mode: masking.MaskModeFull}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != "****" {
		t.Errorf("got %q, want ****", result.Rows[0][0])
	}
}

func TestMaskPartial(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns: []string{"phone"},
		Origins: []masking.ColumnOrigin{{Table: "users", Column: "phone"}},
		Rows:    [][]any{{"13812345678"}},
	}
	rules := []masking.Rule{{Table: "users", Column: "phone", Mode: masking.MaskModePartial}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	got := result.Rows[0][0].(string)
	if !strings.HasPrefix(got, "138") || !strings.HasSuffix(got, "5678") {
		t.Errorf("partial mask unexpected: %q", got)
	}
	if strings.Count(got, "*") != 4 {
		t.Errorf("partial mask should use fixed mask length, got %q", got)
	}
}

func TestMaskPartialWithConfig(t *testing.T) {
	e := newEngine(t)
	config, _ := json.Marshal(map[string]any{
		"keep_prefix": 3,
		"keep_suffix": 4,
		"mask_text":   "......",
	})
	result := &masking.QueryResult{
		Columns: []string{"wallet_address"},
		Origins: []masking.ColumnOrigin{{Table: "wallets", Column: "wallet_address"}},
		Rows:    [][]any{{"0x1234567890ABCDEF"}},
	}
	rules := []masking.Rule{{Table: "wallets", Column: "wallet_address", Mode: masking.MaskModePartial, Config: config}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0][0]; got != "0x1......CDEF" {
		t.Fatalf("got %q", got)
	}
}

func TestMaskPartialWithZeroVisibleSegmentsUsesFixedLength(t *testing.T) {
	e := newEngine(t)
	config, _ := json.Marshal(map[string]any{
		"keep_prefix": 0,
		"keep_suffix": 0,
		"mask_char":   "*",
	})
	result := &masking.QueryResult{
		Columns: []string{"username"},
		Origins: []masking.ColumnOrigin{{Table: "users", Column: "username"}},
		Rows:    [][]any{{"administrator"}},
	}
	rules := []masking.Rule{{Table: "users", Column: "username", Mode: masking.MaskModePartial, Config: config}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0][0]; got != "****" {
		t.Fatalf("got %q, want %q", got, "****")
	}
}

func TestMaskHashDeterministic(t *testing.T) {
	e := newEngine(t)
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeHash}}

	r1 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"alice@example.com"}}}
	e.MaskResult(r1, rules)
	e.MaskResult(r2, rules)
	if r1.Rows[0][0] != r2.Rows[0][0] {
		t.Error("same value should produce same hash")
	}
}

func TestMaskHashDifferentValues(t *testing.T) {
	e := newEngine(t)
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeHash}}

	r1 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"bob@example.com"}}}
	e.MaskResult(r1, rules)
	e.MaskResult(r2, rules)
	if r1.Rows[0][0] == r2.Rows[0][0] {
		t.Error("different values should produce different hashes")
	}
}

func TestMaskHashWithPepperVsWithout(t *testing.T) {
	// TE5: different keys should produce different hashes
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 0xFF

	e1, _ := masking.NewEngine(key1, masking.GlobalCache())
	e2, _ := masking.NewEngine(key2, masking.GlobalCache())
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeHash}}

	r1 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}}, Rows: [][]any{{"alice@example.com"}}}
	e1.MaskResult(r1, rules)
	e2.MaskResult(r2, rules)
	if r1.Rows[0][0] == r2.Rows[0][0] {
		t.Error("same value with different pepper should produce different hashes (TE5)")
	}
}

func TestMaskEmailWithConfig(t *testing.T) {
	e := newEngine(t)
	config, _ := json.Marshal(map[string]any{
		"keep_local_prefix": 1,
		"keep_domain":       true,
		"replacement":       "****",
	})
	result := &masking.QueryResult{
		Columns: []string{"email"},
		Origins: []masking.ColumnOrigin{{Table: "users", Column: "email"}},
		Rows:    [][]any{{"james@gmail.com"}},
	}
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeEmail, Config: config}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0][0]; got != "j****@gmail.com" {
		t.Fatalf("got %q", got)
	}
}

func TestMatchColumnPatternRegex(t *testing.T) {
	matched, err := masking.MatchColumnPattern(masking.Rule{
		Column: "^(wallet_address|from_addr|to_addr)$",
		Match:  masking.MatchTypeRegex,
	}, "from_addr")
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected regex to match from_addr")
	}
}

func TestMaskNilValueSkipped(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns: []string{"phone"},
		Origins: []masking.ColumnOrigin{{Table: "users", Column: "phone"}},
		Rows:    [][]any{{nil}},
	}
	rules := []masking.Rule{{Table: "users", Column: "phone", Mode: masking.MaskModeFull}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != nil {
		t.Error("nil value should remain nil after masking")
	}
}

func TestNoRulesNoChange(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns: []string{"name"},
		Rows:    [][]any{{"Alice"}},
	}
	if err := e.MaskResult(result, nil); err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != "Alice" {
		t.Error("no rules should leave data unchanged")
	}
}

func TestMaskingUsesRawColumnsForLabelMatchingFallback(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns:    []string{"user_id"},
		RawColumns: []string{"t_deposit.user_id"},
		Rows:       [][]any{{"12345"}},
	}
	rules := []masking.Rule{{Table: "t_deposit", Column: "user_id", Mode: masking.MaskModeFull}}
	if err := e.MaskResult(result, rules); err != nil {
		t.Fatal(err)
	}
	if result.Rows[0][0] != "****" {
		t.Errorf("got %q, want ****", result.Rows[0][0])
	}
}
