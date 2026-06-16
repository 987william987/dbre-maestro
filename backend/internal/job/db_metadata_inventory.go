package job

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const inventorySchedulerPollInterval = time.Minute

type DBMetadataInventoryJob struct {
	settings  *repository.SettingsRepo
	snapshots *repository.DBMetadataRepo
	logger    *slog.Logger

	mu        sync.Mutex
	lastRunAt time.Time
	isRunning bool
}

func NewDBMetadataInventoryJob(settings *repository.SettingsRepo, snapshots *repository.DBMetadataRepo, logger *slog.Logger) *DBMetadataInventoryJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &DBMetadataInventoryJob{
		settings:  settings,
		snapshots: snapshots,
		logger:    logger,
	}
}

func (j *DBMetadataInventoryJob) Start(ctx context.Context) {
	ticker := time.NewTicker(inventorySchedulerPollInterval)
	defer ticker.Stop()

	j.runIfDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.runIfDue(ctx)
		}
	}
}

func (j *DBMetadataInventoryJob) runIfDue(ctx context.Context) {
	settings, err := j.settings.Get(ctx)
	if err != nil {
		j.logger.Warn("db metadata inventory: load settings failed", "err", err)
		return
	}
	if !settings.DBMetadataInventoryEnabled {
		return
	}

	intervalMinutes := settings.DBMetadataInventorySyncIntervalMins
	if intervalMinutes <= 0 {
		intervalMinutes = 5
	}

	j.mu.Lock()
	if j.isRunning {
		j.mu.Unlock()
		return
	}
	if !j.lastRunAt.IsZero() && time.Since(j.lastRunAt) < time.Duration(intervalMinutes)*time.Minute {
		j.mu.Unlock()
		return
	}
	j.isRunning = true
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.isRunning = false
		j.lastRunAt = time.Now()
		j.mu.Unlock()
	}()

	if err := j.RunOnce(ctx, settings); err != nil {
		j.logger.Warn("db metadata inventory: run failed", "err", err)
		return
	}
}

func (j *DBMetadataInventoryJob) RunOnce(ctx context.Context, settings *model.PlatformSettings) error {
	regions := normalizeRegions(settings.DBMetadataInventoryRegions)
	engines := normalizeInventoryEngines(settings.DBMetadataInventoryEngines)
	if len(regions) == 0 || len(engines) == 0 {
		j.logger.Info("db metadata inventory: skip empty scope", "regions", regions, "engines", engines)
		return nil
	}

	snapshotAt := time.Now().UTC()
	items := make([]model.CloudDBInventorySnapshot, 0)
	for _, region := range regions {
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return fmt.Errorf("load aws config for region %s: %w", region, err)
		}

		if engines["aurora-mysql"] || engines["aurora-postgresql"] {
			rdsItems, err := collectRDSInventory(ctx, rds.NewFromConfig(cfg), region, engines)
			if err != nil {
				return err
			}
			items = append(items, rdsItems...)
		}

		if engines["redis"] {
			redisItems, err := collectRedisInventory(ctx, elasticache.NewFromConfig(cfg), region)
			if err != nil {
				return err
			}
			items = append(items, redisItems...)
		}
	}

	if err := j.snapshots.ReplaceInventorySnapshots(ctx, snapshotAt, items); err != nil {
		return err
	}

	j.logger.Info("db metadata inventory: snapshots replaced", "count", len(items), "regions", len(regions))
	return nil
}

func collectRDSInventory(ctx context.Context, client *rds.Client, region string, engines map[string]bool) ([]model.CloudDBInventorySnapshot, error) {
	clusterByID := make(map[string]rdstypes.DBCluster)
	clusterMembers := make(map[string]map[string]bool)

	clusterPaginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe db clusters in %s: %w", region, err)
		}
		for _, cluster := range page.DBClusters {
			engine := strings.TrimSpace(valueString(cluster.Engine))
			if !engines[engine] {
				continue
			}
			clusterByID[valueString(cluster.DBClusterIdentifier)] = cluster
			members := make(map[string]bool, len(cluster.DBClusterMembers))
			for _, member := range cluster.DBClusterMembers {
				members[valueString(member.DBInstanceIdentifier)] = member.IsClusterWriter != nil && *member.IsClusterWriter
			}
			clusterMembers[valueString(cluster.DBClusterIdentifier)] = members
		}
	}

	items := make([]model.CloudDBInventorySnapshot, 0)
	instancePaginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for instancePaginator.HasMorePages() {
		page, err := instancePaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe db instances in %s: %w", region, err)
		}
		for _, instance := range page.DBInstances {
			engine := strings.TrimSpace(valueString(instance.Engine))
			if !engines[engine] {
				continue
			}
			clusterID := valueString(instance.DBClusterIdentifier)
			cluster, hasCluster := clusterByID[clusterID]
			if !hasCluster {
				continue
			}
			role := "reader"
			if members, ok := clusterMembers[clusterID]; ok && members[valueString(instance.DBInstanceIdentifier)] {
				role = "writer"
			}
			rawPayload, _ := json.Marshal(struct {
				Cluster  rdstypes.DBCluster  `json:"cluster"`
				Instance rdstypes.DBInstance `json:"instance"`
			}{Cluster: cluster, Instance: instance})
			rawPayloadString := string(rawPayload)

			items = append(items, model.CloudDBInventorySnapshot{
				Provider:              "aws",
				Engine:                engine,
				Region:                region,
				AZ:                    stringPtr(valueString(instance.AvailabilityZone)),
				DBIdentifier:          chooseDBIdentifier(cluster, instance),
				ClusterIdentifier:     stringPtr(clusterID),
				InstanceIdentifier:    stringPtr(valueString(instance.DBInstanceIdentifier)),
				Role:                  stringPtr(role),
				EngineVersion:         stringPtr(valueString(instance.EngineVersion)),
				InstanceClass:         stringPtr(valueString(instance.DBInstanceClass)),
				StorageType:           stringPtr(valueString(instance.StorageType)),
				ClusterEndpoint:       stringPtr(valueString(cluster.Endpoint)),
				ClusterReaderEndpoint: stringPtr(valueString(cluster.ReaderEndpoint)),
				InstanceEndpoint:      stringPtr(valueRDSEndpointAddress(instance.Endpoint)),
				RawPayloadJSON:        &rawPayloadString,
			})
		}
	}

	return items, nil
}

func collectRedisInventory(ctx context.Context, client *elasticache.Client, region string) ([]model.CloudDBInventorySnapshot, error) {
	items := make([]model.CloudDBInventorySnapshot, 0)
	paginator := elasticache.NewDescribeCacheClustersPaginator(client, &elasticache.DescribeCacheClustersInput{
		ShowCacheNodeInfo: boolPtr(true),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe cache clusters in %s: %w", region, err)
		}
		for _, cluster := range page.CacheClusters {
			engine := strings.TrimSpace(valueString(cluster.Engine))
			if engine != "redis" {
				continue
			}
			instanceEndpoint := ""
			az := ""
			if cluster.ConfigurationEndpoint != nil {
				instanceEndpoint = valueElastiCacheEndpointAddress(cluster.ConfigurationEndpoint)
			} else if len(cluster.CacheNodes) > 0 {
				instanceEndpoint = valueElastiCacheEndpointAddress(cluster.CacheNodes[0].Endpoint)
				az = valueString(cluster.CacheNodes[0].CustomerAvailabilityZone)
			}
			rawPayload, _ := json.Marshal(cluster)
			rawPayloadString := string(rawPayload)
			items = append(items, model.CloudDBInventorySnapshot{
				Provider:           "aws",
				Engine:             engine,
				Region:             region,
				AZ:                 stringPtr(az),
				DBIdentifier:       valueString(cluster.CacheClusterId),
				InstanceIdentifier: stringPtr(valueString(cluster.CacheClusterId)),
				Role:               stringPtr("primary"),
				EngineVersion:      stringPtr(valueString(cluster.EngineVersion)),
				InstanceClass:      stringPtr(valueString(cluster.CacheNodeType)),
				InstanceEndpoint:   stringPtr(instanceEndpoint),
				RawPayloadJSON:     &rawPayloadString,
			})
		}
	}
	return items, nil
}

func normalizeRegions(regions []string) []string {
	out := make([]string, 0, len(regions))
	seen := make(map[string]struct{}, len(regions))
	for _, region := range regions {
		trimmed := strings.TrimSpace(region)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeInventoryEngines(engines []string) map[string]bool {
	out := make(map[string]bool, len(engines))
	for _, engine := range engines {
		trimmed := strings.TrimSpace(engine)
		if trimmed == "" {
			continue
		}
		out[trimmed] = true
	}
	return out
}

func chooseDBIdentifier(cluster rdstypes.DBCluster, instance rdstypes.DBInstance) string {
	if id := valueString(cluster.DBClusterIdentifier); id != "" {
		return id
	}
	return valueString(instance.DBInstanceIdentifier)
}

func valueString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueRDSEndpointAddress(endpoint *rdstypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return valueString(endpoint.Address)
}

func valueElastiCacheEndpointAddress(endpoint *elasticachetypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return valueString(endpoint.Address)
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func boolPtr(value bool) *bool {
	return &value
}
