package handler

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestMapInventorySnapshot(t *testing.T) {
	endpoint := "cluster-ro.internal"
	other := "db.internal"

	tests := []struct {
		name        string
		connections []model.DBConnection
		wantStatus  string
		wantCount   int
	}{
		{
			name: "unmatched",
			connections: []model.DBConnection{
				{ID: 1, Name: "analytics", Host: other},
			},
			wantStatus: "unmatched",
			wantCount:  0,
		},
		{
			name: "matched",
			connections: []model.DBConnection{
				{ID: 1, Name: "analytics", Host: endpoint},
			},
			wantStatus: "matched",
			wantCount:  1,
		},
		{
			name: "ambiguous",
			connections: []model.DBConnection{
				{ID: 1, Name: "analytics-ro", Host: endpoint},
				{ID: 2, Name: "analytics-shadow", Host: endpoint},
			},
			wantStatus: "ambiguous",
			wantCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, matches := mapInventorySnapshot(model.CloudDBInventorySnapshot{
				ClusterReaderEndpoint: &endpoint,
			}, tt.connections)
			if status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", status, tt.wantStatus)
			}
			if len(matches) != tt.wantCount {
				t.Fatalf("match count = %d, want %d", len(matches), tt.wantCount)
			}
		})
	}
}
