package masking_test

import (
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
}

func TestMaskHashDeterministic(t *testing.T) {
	e := newEngine(t)
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeHash}}

	r1 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"alice@example.com"}}}
	e.MaskResult(r1, rules)
	e.MaskResult(r2, rules)
	if r1.Rows[0][0] != r2.Rows[0][0] {
		t.Error("same value should produce same hash")
	}
}

func TestMaskHashDifferentValues(t *testing.T) {
	e := newEngine(t)
	rules := []masking.Rule{{Table: "users", Column: "email", Mode: masking.MaskModeHash}}

	r1 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"bob@example.com"}}}
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

	r1 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"alice@example.com"}}}
	r2 := &masking.QueryResult{Columns: []string{"email"}, Rows: [][]any{{"alice@example.com"}}}
	e1.MaskResult(r1, rules)
	e2.MaskResult(r2, rules)
	if r1.Rows[0][0] == r2.Rows[0][0] {
		t.Error("same value with different pepper should produce different hashes (TE5)")
	}
}

func TestMaskNilValueSkipped(t *testing.T) {
	e := newEngine(t)
	result := &masking.QueryResult{
		Columns: []string{"phone"},
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
