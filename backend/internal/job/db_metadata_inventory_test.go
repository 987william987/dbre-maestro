package job

import (
	"context"
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
)

func TestNormalizeRegions(t *testing.T) {
	got := normalizeRegions([]string{
		" ap-northeast-1 ",
		"",
		"ap-southeast-1",
		"ap-northeast-1",
		"  ",
	})

	want := []string{"ap-northeast-1", "ap-southeast-1"}
	if len(got) != len(want) {
		t.Fatalf("len(normalizeRegions) = %d, want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeRegions[%d] = %q, want %q; got=%v", i, got[i], want[i], got)
		}
	}
}

func TestNormalizeInventoryEngines(t *testing.T) {
	got := normalizeInventoryEngines([]string{
		" aurora-mysql ",
		"redis",
		"",
		"aurora-mysql",
		" aurora-postgresql ",
	})

	expectedKeys := []string{"aurora-mysql", "aurora-postgresql", "redis"}
	if len(got) != len(expectedKeys) {
		t.Fatalf("len(normalizeInventoryEngines) = %d, want %d; got=%v", len(got), len(expectedKeys), got)
	}
	for _, key := range expectedKeys {
		if !got[key] {
			t.Fatalf("normalizeInventoryEngines missing key %q; got=%v", key, got)
		}
	}
}

func TestRunOnceSkipsWhenInventoryScopeIsEmpty(t *testing.T) {
	job := NewDBMetadataInventoryJob(nil, nil, nil)

	err := job.RunOnce(context.Background(), &model.PlatformSettings{
		DBMetadataInventoryEnabled:          true,
		DBMetadataInventoryRegions:          []string{},
		DBMetadataInventoryEngines:          []string{"aurora-mysql"},
		DBMetadataInventorySyncIntervalMins: 5,
	})
	if err != nil {
		t.Fatalf("RunOnce with empty regions returned error: %v", err)
	}

	err = job.RunOnce(context.Background(), &model.PlatformSettings{
		DBMetadataInventoryEnabled:          true,
		DBMetadataInventoryRegions:          []string{"ap-northeast-1"},
		DBMetadataInventoryEngines:          []string{},
		DBMetadataInventorySyncIntervalMins: 5,
	})
	if err != nil {
		t.Fatalf("RunOnce with empty engines returned error: %v", err)
	}
}
