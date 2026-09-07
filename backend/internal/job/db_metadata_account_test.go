package job

import "testing"

func TestQuoteMySQLAccountPartEscapesBackticks(t *testing.T) {
	if got, want := quoteMySQLAccountPart("reader`ops"), "`reader``ops`"; got != want {
		t.Fatalf("quoteMySQLAccountPart() = %q, want %q", got, want)
	}
}
