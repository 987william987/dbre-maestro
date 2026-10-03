package tableschema

import "testing"

func TestFoundationLimitsMatchSchemaManagementSafetyBoundary(t *testing.T) {
	if MaxTables != 50 {
		t.Fatalf("MaxTables = %d, want 50 so one request cannot silently expand beyond the reviewed batch limit", MaxTables)
	}
	if PreviewTTL.Seconds() != 60 {
		t.Fatalf("PreviewTTL = %s, want 60s so execution cannot rely on a stale schema snapshot", PreviewTTL)
	}
}

func TestStableErrorIncludesMachineCode(t *testing.T) {
	err := (&StableError{Code: ErrorInvalidTableSelection, Message: "select at least one table"}).Error()
	if err != "invalid_table_selection: select at least one table" {
		t.Fatalf("Error() = %q", err)
	}
}
