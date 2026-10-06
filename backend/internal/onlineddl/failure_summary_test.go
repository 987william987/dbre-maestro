package onlineddl

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFailureSummaryUsesStderrAndRetainsMySQLError(t *testing.T) {
	result := ToolResult{
		Stdout: "ERROR stdout fallback",
		Stderr: "noise\nDBD::mysql::db do failed: Error 1419: You do not have the SUPER privilege and binary logging is enabled\n",
	}
	want := "DBD::mysql::db do failed: Error 1419: You do not have the SUPER privilege and binary logging is enabled"
	if got := FailureSummary(result); got != want {
		t.Fatalf("summary=%q", got)
	}
}

func TestFailureSummaryRedactsSensitiveValuesAndIsBounded(t *testing.T) {
	secret := "top-secret"
	output := "ERROR password=" + secret + " DBI:mysql:database=app;host=db.internal --defaults-file=/tmp/maestro-online-ddl-123/client.cnf " + strings.Repeat("x", 2000)
	got := FailureSummary(ToolResult{Stderr: output})
	for _, forbidden := range []string{secret, "DBI:mysql", "/tmp/maestro-online-ddl", "client.cnf"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("summary leaked %q: %q", forbidden, got)
		}
	}
	if len(got) > maxFailureSummaryBytes || !strings.HasSuffix(got, "...") {
		t.Fatalf("summary length=%d", len(got))
	}
}

func TestFailureSummaryIgnoresNonErrorOutput(t *testing.T) {
	if got := FailureSummary(ToolResult{Stdout: "Copy: 50%; State: migrating"}); got != "" {
		t.Fatalf("summary=%q", got)
	}
}

func TestFailureSummaryTruncatesUTF8Safely(t *testing.T) {
	got := FailureSummary(ToolResult{Stderr: "ERROR " + strings.Repeat("權限不足", 400)})
	if !utf8.ValidString(got) || len(got) > maxFailureSummaryBytes {
		t.Fatalf("invalid bounded summary: bytes=%d", len(got))
	}
}
