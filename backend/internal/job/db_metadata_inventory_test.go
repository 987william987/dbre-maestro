package job

import (
	"context"
	"testing"

	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
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

func TestMergeRedisReplicationGroupIndexesRolesAndEndpoints(t *testing.T) {
	configEndpoint := "redis-cluster.cfg.cache.amazonaws.com"
	primaryEndpoint := "redis-primary.cache.amazonaws.com"
	readerEndpoint := "redis-reader.cache.amazonaws.com"
	primaryClusterID := "redis-0001-001"
	replicaClusterID := "redis-0001-002"
	index := map[string]redisReplicationNode{}

	mergeRedisReplicationGroup(index, elasticachetypes.ReplicationGroup{
		Engine:                stringPtr("redis"),
		ConfigurationEndpoint: &elasticachetypes.Endpoint{Address: &configEndpoint},
		NodeGroups: []elasticachetypes.NodeGroup{{
			PrimaryEndpoint: &elasticachetypes.Endpoint{Address: &primaryEndpoint},
			ReaderEndpoint:  &elasticachetypes.Endpoint{Address: &readerEndpoint},
			NodeGroupMembers: []elasticachetypes.NodeGroupMember{
				{CacheClusterId: &primaryClusterID, CurrentRole: stringPtr("primary")},
				{CacheClusterId: &replicaClusterID, CurrentRole: stringPtr("replica")},
			},
		}},
	})

	primary := index[primaryClusterID]
	if primary.role != "primary" {
		t.Fatalf("primary role = %q, want primary", primary.role)
	}
	if primary.clusterEndpoint() != configEndpoint {
		t.Fatalf("primary cluster endpoint = %q, want %q", primary.clusterEndpoint(), configEndpoint)
	}
	if primary.readerEndpoint != readerEndpoint {
		t.Fatalf("primary reader endpoint = %q, want %q", primary.readerEndpoint, readerEndpoint)
	}

	replica := index[replicaClusterID]
	if replica.role != "replica" {
		t.Fatalf("replica role = %q, want replica", replica.role)
	}
	if replica.clusterEndpoint() != configEndpoint {
		t.Fatalf("replica cluster endpoint = %q, want %q", replica.clusterEndpoint(), configEndpoint)
	}
	if replica.readerEndpoint != readerEndpoint {
		t.Fatalf("replica reader endpoint = %q, want %q", replica.readerEndpoint, readerEndpoint)
	}
}

func TestMergeRedisReplicationGroupUsesNodeRoleWhenClusterModeDoesNotExposeCurrentRole(t *testing.T) {
	configEndpoint := "redis-cluster.cfg.cache.amazonaws.com"
	clusterID := "redis-0001-001"
	index := map[string]redisReplicationNode{}

	mergeRedisReplicationGroup(index, elasticachetypes.ReplicationGroup{
		Engine:                stringPtr("redis"),
		ConfigurationEndpoint: &elasticachetypes.Endpoint{Address: &configEndpoint},
		NodeGroups: []elasticachetypes.NodeGroup{{
			NodeGroupMembers: []elasticachetypes.NodeGroupMember{
				{CacheClusterId: &clusterID},
			},
		}},
	})

	node := index[clusterID]
	if node.role != "node" {
		t.Fatalf("role = %q, want node for cluster-mode members without CurrentRole", node.role)
	}
	if node.clusterEndpoint() != configEndpoint {
		t.Fatalf("cluster endpoint = %q, want %q", node.clusterEndpoint(), configEndpoint)
	}
	if node.readerEndpoint != "" {
		t.Fatalf("reader endpoint = %q, want empty", node.readerEndpoint)
	}
}
