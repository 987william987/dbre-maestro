package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sessionmanagement"
)

func TestSessionIdentityMatchesRejectsReusedOrChangedSession(t *testing.T) {
	current := sessionmanagement.Session{User: "app", Database: "orders", Client: "10.0.0.8", QueryHash: "hash", BackendStart: "2026-10-02 12:00:00+00"}
	expected := sessionIdentityRequest{User: current.User, Database: current.Database, Client: current.Client, QueryHash: current.QueryHash, BackendStart: current.BackendStart}
	if !sessionIdentityMatches(current, expected) {
		t.Fatal("unchanged session identity must match before a destructive action")
	}

	cases := map[string]func(*sessionIdentityRequest){
		"user":          func(value *sessionIdentityRequest) { value.User = "other" },
		"database":      func(value *sessionIdentityRequest) { value.Database = "other" },
		"client":        func(value *sessionIdentityRequest) { value.Client = "10.0.0.9" },
		"query hash":    func(value *sessionIdentityRequest) { value.QueryHash = "other" },
		"backend start": func(value *sessionIdentityRequest) { value.BackendStart = "2026-10-02 12:01:00+00" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := expected
			mutate(&changed)
			if sessionIdentityMatches(current, changed) {
				t.Fatal("changed identity must be rejected to avoid signaling a reused session ID")
			}
		})
	}
}

func TestSessionSignalAuditDetailsNeverContainRawQuery(t *testing.T) {
	conn := &model.DBConnection{DBType: "postgres"}
	target := &model.DBConnection{Host: "db.internal", Port: 5432}
	current := &sessionmanagement.Session{User: "app", Database: "orders", Client: "10.0.0.8", Query: "SELECT * FROM customers WHERE email = 'will@example.com'", QueryHash: "hash"}
	details := sessionSignalAuditDetails(conn, target, "42", current, "succeeded", "", 125*time.Millisecond)
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := details["query"]; exists || strings.Contains(string(raw), current.Query) || strings.Contains(string(raw), "will@example.com") || details["query_hash"] != "hash" {
		t.Fatalf("audit details must contain only the query hash, got %s", raw)
	}
	if details["duration_ms"] != int64(125) {
		t.Fatalf("duration_ms = %#v, want 125", details["duration_ms"])
	}
}

func TestSessionTargetKeyBindsModeAndCanonicalManualHost(t *testing.T) {
	manual := sessionTargetKey(sessionTargetRequest{Mode: " manual ", Host: " DB.Internal ", Port: 3306})
	aws := sessionTargetKey(sessionTargetRequest{Mode: "aws", Region: "ap-northeast-1", ClusterID: "orders", NodeID: "orders-1"})
	invalid := sessionTargetKey(sessionTargetRequest{Mode: "other", Region: "ap-northeast-1", ClusterID: "orders", NodeID: "orders-1"})
	if manual != "manual|db.internal|3306" || aws != "aws|ap-northeast-1|orders|orders-1" || invalid != "invalid|other" {
		t.Fatalf("target keys = (%q, %q, %q)", manual, aws, invalid)
	}
}

func TestPrefixSummaryAuditDetailsDoNotContainTokenOrRawPrefix(t *testing.T) {
	preview := &sessionmanagement.PrefixPreview{Database: "orders", NormalizedPrefix: "select * from orders where email = 'will@example.com'", Matches: []sessionmanagement.Session{{ID: "42"}}}
	details := prefixSummaryAuditDetails("mysql", sessionTargetRequest{Mode: "manual", Host: "db.internal", Port: 3306}, preview, "succeeded", "", 1, 0, 0, 20*time.Millisecond)
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "will@example.com") || strings.Contains(string(raw), "preview_token") || details["prefix_hash"] == "" {
		t.Fatalf("prefix audit details leaked sensitive input or omitted hash: %s", raw)
	}
}
