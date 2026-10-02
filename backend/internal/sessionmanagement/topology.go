package sessionmanagement

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/netguard"
)

var (
	ErrTargetNotOwned    = errors.New("AWS target is not owned by DB connection")
	ErrInvalidManualHost = errors.New("invalid manual target host")
)

type Node struct {
	ID     string `json:"id"`
	Role   string `json:"role"`
	Host   string `json:"host"`
	Port   uint16 `json:"port"`
	AZ     string `json:"availability_zone,omitempty"`
	Status string `json:"status,omitempty"`
}

type Cluster struct {
	ID             string `json:"id"`
	Engine         string `json:"engine"`
	Region         string `json:"region"`
	Endpoint       string `json:"endpoint,omitempty"`
	ReaderEndpoint string `json:"reader_endpoint,omitempty"`
	Nodes          []Node `json:"nodes"`
}

type Discoverer interface {
	Discover(context.Context, string, string) ([]Cluster, error)
}

type Service struct {
	discoverer Discoverer
	hostPolicy *netguard.Policy
	mu         sync.Mutex
	cache      map[string]cachedTopology
}

type cachedTopology struct {
	items     []Cluster
	expiresAt time.Time
}

func NewService(discoverer Discoverer, hostPolicy *netguard.Policy) *Service {
	return &Service{discoverer: discoverer, hostPolicy: hostPolicy, cache: map[string]cachedTopology{}}
}

func (s *Service) OwnedClusters(ctx context.Context, conn *model.DBConnection, regions []string) ([]Cluster, error) {
	engine, err := topologyEngine(conn)
	if err != nil {
		return nil, err
	}
	items := make([]Cluster, 0)
	for _, region := range normalizedRegions(regions) {
		clusters, err := s.discover(ctx, region, engine, true)
		if err != nil {
			return nil, fmt.Errorf("discover AWS topology in %s: %w", region, err)
		}
		for _, cluster := range clusters {
			if connectionOwnsCluster(conn, cluster) {
				items = append(items, cluster)
			}
		}
	}
	return items, nil
}

func (s *Service) OwnedTopology(ctx context.Context, conn *model.DBConnection, regions []string, region, clusterID string) (*Cluster, error) {
	region = strings.TrimSpace(region)
	clusterID = strings.TrimSpace(clusterID)
	if region == "" || clusterID == "" || !contains(normalizedRegions(regions), region) {
		return nil, ErrTargetNotOwned
	}
	engine, err := topologyEngine(conn)
	if err != nil {
		return nil, err
	}
	clusters, err := s.discover(ctx, region, engine, true)
	if err != nil {
		return nil, fmt.Errorf("discover AWS topology in %s: %w", region, err)
	}
	for i := range clusters {
		if clusters[i].ID == clusterID && connectionOwnsCluster(conn, clusters[i]) {
			return &clusters[i], nil
		}
	}
	return nil, ErrTargetNotOwned
}

func (s *Service) ResolveOwnedNode(ctx context.Context, conn *model.DBConnection, regions []string, region, clusterID, nodeID string) (*Node, error) {
	return s.resolveOwnedNode(ctx, conn, regions, region, clusterID, nodeID, false)
}

func (s *Service) ResolveOwnedNodeLive(ctx context.Context, conn *model.DBConnection, regions []string, region, clusterID, nodeID string) (*Node, error) {
	return s.resolveOwnedNode(ctx, conn, regions, region, clusterID, nodeID, true)
}

func (s *Service) resolveOwnedNode(ctx context.Context, conn *model.DBConnection, regions []string, region, clusterID, nodeID string, force bool) (*Node, error) {
	region, clusterID, nodeID = strings.TrimSpace(region), strings.TrimSpace(clusterID), strings.TrimSpace(nodeID)
	if region == "" || clusterID == "" || nodeID == "" || !contains(normalizedRegions(regions), region) {
		return nil, ErrTargetNotOwned
	}
	engine, err := topologyEngine(conn)
	if err != nil {
		return nil, err
	}
	clusters, err := s.discover(ctx, region, engine, force)
	if err != nil {
		return nil, fmt.Errorf("discover AWS topology in %s: %w", region, err)
	}
	for _, cluster := range clusters {
		if cluster.ID != clusterID || !connectionOwnsCluster(conn, cluster) {
			continue
		}
		for i := range cluster.Nodes {
			if cluster.Nodes[i].ID == nodeID && cluster.Nodes[i].Host != "" && cluster.Nodes[i].Port != 0 {
				return &cluster.Nodes[i], nil
			}
		}
	}
	return nil, ErrTargetNotOwned
}

func (s *Service) discover(ctx context.Context, region, engine string, force bool) ([]Cluster, error) {
	key := region + "|" + engine
	if !force {
		s.mu.Lock()
		cached, ok := s.cache[key]
		s.mu.Unlock()
		if ok && time.Now().Before(cached.expiresAt) {
			return cached.items, nil
		}
	}
	items, err := s.discoverer.Discover(ctx, region, engine)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[key] = cachedTopology{items: items, expiresAt: time.Now().Add(30 * time.Second)}
	s.mu.Unlock()
	return items, nil
}

func (s *Service) ValidateManualTarget(ctx context.Context, host string, port uint16) (netguard.CheckReport, error) {
	host = strings.TrimSpace(host)
	if port == 0 || !validHost(host) {
		return netguard.CheckReport{}, ErrInvalidManualHost
	}
	if s.hostPolicy == nil {
		return netguard.CheckReport{Endpoint: "session-management-manual", Host: host, Port: port}, nil
	}
	return s.hostPolicy.Check(ctx, "session-management-manual", host, port)
}

func topologyEngine(conn *model.DBConnection) (string, error) {
	if conn == nil {
		return "", errors.New("DB connection is required")
	}
	switch strings.ToLower(strings.TrimSpace(conn.DBType)) {
	case "mysql":
		return "mysql", nil
	case "postgres", "postgresql":
		return "postgres", nil
	case "redis":
		return "redis", nil
	default:
		return "", fmt.Errorf("unsupported DB type %q", conn.DBType)
	}
}

func connectionOwnsCluster(conn *model.DBConnection, cluster Cluster) bool {
	owned := map[string]struct{}{
		normalizeHost(conn.EffectiveReadonlyHost()):  {},
		normalizeHost(conn.EffectiveReadwriteHost()): {},
	}
	for _, endpoint := range clusterEndpoints(cluster) {
		if _, ok := owned[normalizeHost(endpoint)]; ok && normalizeHost(endpoint) != "" {
			return true
		}
	}
	return false
}

func clusterEndpoints(cluster Cluster) []string {
	items := []string{cluster.Endpoint, cluster.ReaderEndpoint}
	for _, node := range cluster.Nodes {
		items = append(items, node.Host)
	}
	return items
}

func normalizedRegions(regions []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(regions))
	for _, raw := range regions {
		region := strings.TrimSpace(raw)
		if region == "" {
			continue
		}
		if _, ok := seen[region]; ok {
			continue
		}
		seen[region] = struct{}{}
		result = append(result, region)
	}
	return result
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func validHost(host string) bool {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/\\@?#: \t\r\n") {
		return net.ParseIP(host) != nil
	}
	return true
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

type rdsAPI interface {
	DescribeDBClusters(context.Context, *rds.DescribeDBClustersInput, ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error)
	DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}

type elasticacheAPI interface {
	DescribeReplicationGroups(context.Context, *elasticache.DescribeReplicationGroupsInput, ...func(*elasticache.Options)) (*elasticache.DescribeReplicationGroupsOutput, error)
	DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error)
}

type AWSDiscoverer struct{}

func NewAWSDiscoverer() *AWSDiscoverer { return &AWSDiscoverer{} }

func (d *AWSDiscoverer) Discover(ctx context.Context, region, engine string) ([]Cluster, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	if engine == "redis" {
		return discoverRedis(ctx, elasticache.NewFromConfig(cfg), region)
	}
	return discoverRDS(ctx, rds.NewFromConfig(cfg), region, engine)
}

func discoverRDS(ctx context.Context, client rdsAPI, region, engine string) ([]Cluster, error) {
	clusters := map[string]*Cluster{}
	writers := map[string]map[string]bool{}
	var clusterMarker *string
	for {
		out, err := client.DescribeDBClusters(ctx, &rds.DescribeDBClustersInput{Marker: clusterMarker})
		if err != nil {
			return nil, fmt.Errorf("describe DB clusters: %w", err)
		}
		for _, item := range out.DBClusters {
			if !matchesRDSEngine(engine, value(item.Engine)) {
				continue
			}
			id := value(item.DBClusterIdentifier)
			clusters[id] = &Cluster{ID: id, Engine: value(item.Engine), Region: region, Endpoint: endpointAddress(item.Endpoint), ReaderEndpoint: endpointAddress(item.ReaderEndpoint), Nodes: []Node{}}
			writers[id] = map[string]bool{}
			for _, member := range item.DBClusterMembers {
				writers[id][value(member.DBInstanceIdentifier)] = member.IsClusterWriter != nil && *member.IsClusterWriter
			}
		}
		clusterMarker = out.Marker
		if clusterMarker == nil || value(clusterMarker) == "" {
			break
		}
	}
	var instanceMarker *string
	for {
		out, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{Marker: instanceMarker})
		if err != nil {
			return nil, fmt.Errorf("describe DB instances: %w", err)
		}
		for _, item := range out.DBInstances {
			cluster := clusters[value(item.DBClusterIdentifier)]
			if cluster == nil {
				if value(item.DBClusterIdentifier) != "" || !matchesRDSEngine(engine, value(item.Engine)) {
					continue
				}
				id := value(item.DBInstanceIdentifier)
				host := rdsEndpoint(item.Endpoint)
				clusters[id] = &Cluster{ID: id, Engine: value(item.Engine), Region: region, Endpoint: host, Nodes: []Node{{ID: id, Role: "standalone", Host: host, Port: rdsEndpointPort(item.Endpoint), AZ: value(item.AvailabilityZone), Status: value(item.DBInstanceStatus)}}}
				continue
			}
			id := value(item.DBInstanceIdentifier)
			role := "reader"
			if writers[cluster.ID][id] {
				role = "writer"
			}
			cluster.Nodes = append(cluster.Nodes, Node{ID: id, Role: role, Host: rdsEndpoint(item.Endpoint), Port: rdsEndpointPort(item.Endpoint), AZ: value(item.AvailabilityZone), Status: value(item.DBInstanceStatus)})
		}
		instanceMarker = out.Marker
		if instanceMarker == nil || value(instanceMarker) == "" {
			break
		}
	}
	return clusterValues(clusters), nil
}

func discoverRedis(ctx context.Context, client elasticacheAPI, region string) ([]Cluster, error) {
	clusters := map[string]*Cluster{}
	memberRoles := map[string]string{}
	var groupMarker *string
	for {
		out, err := client.DescribeReplicationGroups(ctx, &elasticache.DescribeReplicationGroupsInput{Marker: groupMarker})
		if err != nil {
			return nil, fmt.Errorf("describe replication groups: %w", err)
		}
		for _, group := range out.ReplicationGroups {
			id := value(group.ReplicationGroupId)
			cluster := &Cluster{ID: id, Engine: "redis", Region: region, Endpoint: elasticacheEndpoint(group.ConfigurationEndpoint), Nodes: []Node{}}
			for _, nodeGroup := range group.NodeGroups {
				if cluster.Endpoint == "" {
					cluster.Endpoint = elasticacheEndpoint(nodeGroup.PrimaryEndpoint)
				}
				if cluster.ReaderEndpoint == "" {
					cluster.ReaderEndpoint = elasticacheEndpoint(nodeGroup.ReaderEndpoint)
				}
				for _, member := range nodeGroup.NodeGroupMembers {
					memberRoles[value(member.CacheClusterId)] = value(member.CurrentRole)
				}
			}
			clusters[id] = cluster
		}
		groupMarker = out.Marker
		if groupMarker == nil || value(groupMarker) == "" {
			break
		}
	}
	var cacheMarker *string
	for {
		out, err := client.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{Marker: cacheMarker, ShowCacheNodeInfo: boolPointer(true)})
		if err != nil {
			return nil, fmt.Errorf("describe cache clusters: %w", err)
		}
		for _, item := range out.CacheClusters {
			id := value(item.CacheClusterId)
			groupID := value(item.ReplicationGroupId)
			cluster := clusters[groupID]
			if cluster == nil {
				cluster = &Cluster{ID: id, Engine: "redis", Region: region, Endpoint: elasticacheEndpoint(item.ConfigurationEndpoint), Nodes: []Node{}}
				clusters[id] = cluster
			}
			host, port, az := "", uint16(0), ""
			if len(item.CacheNodes) > 0 {
				host = elasticacheEndpoint(item.CacheNodes[0].Endpoint)
				port = elasticacheEndpointPort(item.CacheNodes[0].Endpoint)
				az = value(item.CacheNodes[0].CustomerAvailabilityZone)
			}
			if host == "" {
				host = elasticacheEndpoint(item.ConfigurationEndpoint)
				port = elasticacheEndpointPort(item.ConfigurationEndpoint)
			}
			role := memberRoles[id]
			if role == "" {
				role = "standalone"
			}
			cluster.Nodes = append(cluster.Nodes, Node{ID: id, Role: role, Host: host, Port: port, AZ: az, Status: value(item.CacheClusterStatus)})
		}
		cacheMarker = out.Marker
		if cacheMarker == nil || value(cacheMarker) == "" {
			break
		}
	}
	return clusterValues(clusters), nil
}

func clusterValues(index map[string]*Cluster) []Cluster {
	items := make([]Cluster, 0, len(index))
	for _, item := range index {
		sort.Slice(item.Nodes, func(i, j int) bool { return item.Nodes[i].ID < item.Nodes[j].ID })
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func matchesRDSEngine(family, engine string) bool {
	switch family {
	case "mysql":
		return engine == "mysql" || engine == "aurora" || engine == "aurora-mysql"
	case "postgres":
		return engine == "postgres" || engine == "aurora-postgresql"
	default:
		return false
	}
}

func value(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
func endpointAddress(v *string) string { return value(v) }
func elasticacheEndpoint(v *elasticachetypes.Endpoint) string {
	if v == nil {
		return ""
	}
	return value(v.Address)
}
func elasticacheEndpointPort(v *elasticachetypes.Endpoint) uint16 {
	if v == nil {
		return 0
	}
	return uint16Value(v.Port)
}
func rdsEndpoint(v *rdstypes.Endpoint) string {
	if v == nil {
		return ""
	}
	return value(v.Address)
}
func rdsEndpointPort(v *rdstypes.Endpoint) uint16 {
	if v == nil {
		return 0
	}
	return uint16Value(v.Port)
}
func uint16Value(v *int32) uint16 {
	if v == nil || *v < 1 || *v > 65535 {
		return 0
	}
	return uint16(*v)
}
func boolPointer(v bool) *bool { return &v }
