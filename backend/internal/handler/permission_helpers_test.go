package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestReviewPermissionsForTicket(t *testing.T) {
	testCases := []struct {
		name       string
		ticketType model.TicketType
		want       []string
	}{
		{
			name:       "sql export uses export review permission",
			ticketType: model.TicketTypeSQLExport,
			want:       []string{permissionSQLEditorExportReview},
		},
		{
			name:       "sensitive query access uses sensitive review permission",
			ticketType: model.TicketTypeSensitiveQueryAccess,
			want:       []string{permissionSQLEditorSensitiveRev},
		},
		{
			name:       "ddl falls back to ticket review permission",
			ticketType: model.TicketTypeDDL,
			want:       []string{permissionTicketReview},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := reviewPermissionsForTicket(testCase.ticketType)
			if len(got) != len(testCase.want) {
				t.Fatalf("permission count = %d, want %d", len(got), len(testCase.want))
			}
			for index := range got {
				if got[index] != testCase.want[index] {
					t.Fatalf("permission[%d] = %q, want %q", index, got[index], testCase.want[index])
				}
			}
		})
	}
}
