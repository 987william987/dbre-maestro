package job

import (
	"context"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestNormalizeObjectConnectionIDs(t *testing.T) {
	got := normalizeObjectConnectionIDs([]uint64{0, 18, 12, 18, 12, 7})
	want := []uint64{18, 12, 7}

	if len(got) != len(want) {
		t.Fatalf("len(normalizeObjectConnectionIDs) = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeObjectConnectionIDs[%d] = %d, want %d; got=%v", i, got[i], want[i], got)
		}
	}
}

func TestFilterObjectConnections(t *testing.T) {
	connections := []model.DBConnection{
		{ID: 7, Name: "mysql-a"},
		{ID: 12, Name: "pg-a"},
		{ID: 18, Name: "mysql-b"},
	}

	got := filterObjectConnections(connections, []uint64{18, 7})
	if len(got) != 2 {
		t.Fatalf("len(filterObjectConnections) = %d, want 2; got=%v", len(got), got)
	}
	if got[0].ID != 7 || got[1].ID != 18 {
		t.Fatalf("filterObjectConnections order/content mismatch; got IDs=%v", []uint64{got[0].ID, got[1].ID})
	}
}

func TestObjectRunOnceSkipsWhenScopeIsEmpty(t *testing.T) {
	job := NewDBMetadataObjectJob(nil, nil, nil, nil)

	err := job.RunOnce(context.Background(), &model.PlatformSettings{
		DBMetadataObjectEnabled:              true,
		DBMetadataObjectEnabledConnectionIDs: []uint64{},
		DBMetadataObjectSyncIntervalMins:     5,
	})
	if err != nil {
		t.Fatalf("RunOnce with empty connection ids returned error: %v", err)
	}
}

func TestIsObjectMetadataSupported(t *testing.T) {
	if !isObjectMetadataSupported("mysql") {
		t.Fatal("mysql should be supported")
	}
	if !isObjectMetadataSupported("postgresql") {
		t.Fatal("postgresql should be supported")
	}
	if isObjectMetadataSupported("redis") {
		t.Fatal("redis should not be supported")
	}
}
