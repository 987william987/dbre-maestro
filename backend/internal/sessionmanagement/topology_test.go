package sessionmanagement

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/netguard"
)

type fakeDiscoverer struct {
	items []Cluster
	err   error
	calls int
}

func (f *fakeDiscoverer) Discover(context.Context, string, string) ([]Cluster, error) {
	f.calls++
	return f.items, f.err
}

func TestOwnedClustersOnlyReturnsEndpointOwnedCluster(t *testing.T) {
	discoverer := &fakeDiscoverer{items: []Cluster{
		{ID: "owned", Region: "ap-northeast-1", Endpoint: "writer.example.rds.amazonaws.com"},
		{ID: "forged", Region: "ap-northeast-1", Endpoint: "other.example.rds.amazonaws.com"},
	}}
	service := NewService(discoverer, nil)
	conn := &model.DBConnection{DBType: "mysql", ReadwriteHost: "WRITER.EXAMPLE.RDS.AMAZONAWS.COM."}

	items, err := service.OwnedClusters(context.Background(), conn, []string{"ap-northeast-1"})
	if err != nil {
		t.Fatalf("OwnedClusters() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "owned" {
		t.Fatalf("OwnedClusters() = %#v, want only owned cluster", items)
	}
}

func TestOwnedTopologyRejectsForgedCluster(t *testing.T) {
	service := NewService(&fakeDiscoverer{items: []Cluster{{ID: "forged", Endpoint: "other.example"}}}, nil)
	conn := &model.DBConnection{DBType: "postgres", ReadonlyHost: "owned.example"}

	_, err := service.OwnedTopology(context.Background(), conn, []string{"ap-northeast-1"}, "ap-northeast-1", "forged")
	if !errors.Is(err, ErrTargetNotOwned) {
		t.Fatalf("OwnedTopology() error = %v, want ErrTargetNotOwned", err)
	}
}

func TestOwnedClustersFailsLoudWhenAWSDiscoveryFails(t *testing.T) {
	discoverer := &fakeDiscoverer{err: errors.New("access denied")}
	service := NewService(discoverer, nil)
	conn := &model.DBConnection{DBType: "redis", ReadonlyHost: "redis.example"}

	items, err := service.OwnedClusters(context.Background(), conn, []string{"ap-northeast-1"})
	if err == nil || items != nil || discoverer.calls != 1 {
		t.Fatalf("OwnedClusters() = (%#v, %v), calls=%d; want loud failure", items, err, discoverer.calls)
	}
}

func TestResolveOwnedNodeReusesRecentTopologySnapshot(t *testing.T) {
	discoverer := &fakeDiscoverer{items: []Cluster{{ID: "orders", Endpoint: "orders.cluster", Nodes: []Node{{ID: "orders-1", Host: "orders-1.node", Port: 3306}}}}}
	service := NewService(discoverer, nil)
	conn := &model.DBConnection{DBType: "mysql", ReadonlyHost: "orders.cluster"}
	if _, err := service.OwnedClusters(context.Background(), conn, []string{"ap-northeast-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveOwnedNode(context.Background(), conn, []string{"ap-northeast-1"}, "ap-northeast-1", "orders", "orders-1"); err != nil {
		t.Fatal(err)
	}
	if discoverer.calls != 1 {
		t.Fatalf("AWS discovery calls = %d, want 1 within topology cache TTL", discoverer.calls)
	}
}

type staticResolver struct{ ips []net.IPAddr }

func (r staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.ips, nil
}

func TestValidateManualTargetAlwaysChecksSyntaxAndConfiguredPolicy(t *testing.T) {
	policy, err := netguard.NewPolicy(netguard.Config{Enforcement: "enforce", HostAllowlist: []string{"*.internal"}, CIDRAllowlist: []string{"10.0.0.0/8"}})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	service := NewService(&fakeDiscoverer{}, policy.WithResolver(staticResolver{ips: []net.IPAddr{{IP: net.ParseIP("192.168.1.10")}}}))

	if _, err := service.ValidateManualTarget(context.Background(), "https://db.internal", 3306); !errors.Is(err, ErrInvalidManualHost) {
		t.Fatalf("invalid syntax error = %v, want ErrInvalidManualHost", err)
	}
	if report, err := service.ValidateManualTarget(context.Background(), "db.internal", 3306); err == nil || len(report.Violations) == 0 {
		t.Fatalf("policy result = (%#v, %v), want blocked CIDR", report, err)
	}
}

type fakeRDS struct {
	clusters  *rds.DescribeDBClustersOutput
	instances *rds.DescribeDBInstancesOutput
}

func (f fakeRDS) DescribeDBClusters(context.Context, *rds.DescribeDBClustersInput, ...func(*rds.Options)) (*rds.DescribeDBClustersOutput, error) {
	return f.clusters, nil
}
func (f fakeRDS) DescribeDBInstances(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	return f.instances, nil
}

func TestDiscoverRDSBuildsWriterAndReaderNodes(t *testing.T) {
	client := fakeRDS{
		clusters: &rds.DescribeDBClustersOutput{DBClusters: []rdstypes.DBCluster{{
			DBClusterIdentifier: aws.String("orders"), Engine: aws.String("aurora-mysql"), Endpoint: aws.String("orders.cluster"), ReaderEndpoint: aws.String("orders.reader"),
			DBClusterMembers: []rdstypes.DBClusterMember{{DBInstanceIdentifier: aws.String("orders-1"), IsClusterWriter: aws.Bool(true)}, {DBInstanceIdentifier: aws.String("orders-2")}},
		}}},
		instances: &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{
			{DBClusterIdentifier: aws.String("orders"), DBInstanceIdentifier: aws.String("orders-2"), Endpoint: &rdstypes.Endpoint{Address: aws.String("reader.node"), Port: aws.Int32(3306)}},
			{DBClusterIdentifier: aws.String("orders"), DBInstanceIdentifier: aws.String("orders-1"), Endpoint: &rdstypes.Endpoint{Address: aws.String("writer.node"), Port: aws.Int32(3306)}},
		}},
	}

	items, err := discoverRDS(context.Background(), client, "ap-northeast-1", "mysql")
	if err != nil {
		t.Fatalf("discoverRDS() error = %v", err)
	}
	if len(items) != 1 || len(items[0].Nodes) != 2 || items[0].Nodes[0].Role != "writer" || items[0].Nodes[1].Role != "reader" {
		t.Fatalf("discoverRDS() = %#v", items)
	}
}

func TestDiscoverRDSIncludesStandaloneInstance(t *testing.T) {
	client := fakeRDS{
		clusters: &rds.DescribeDBClustersOutput{},
		instances: &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{
			DBInstanceIdentifier: aws.String("legacy-mysql"), Engine: aws.String("mysql"), Endpoint: &rdstypes.Endpoint{Address: aws.String("legacy.node"), Port: aws.Int32(3306)},
		}}},
	}

	items, err := discoverRDS(context.Background(), client, "ap-northeast-1", "mysql")
	if err != nil {
		t.Fatalf("discoverRDS() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "legacy-mysql" || len(items[0].Nodes) != 1 || items[0].Nodes[0].Role != "standalone" {
		t.Fatalf("discoverRDS() = %#v", items)
	}
}

type redisFixtureClient struct {
	groups *elasticache.DescribeReplicationGroupsOutput
	caches *elasticache.DescribeCacheClustersOutput
}

func (f redisFixtureClient) DescribeReplicationGroups(context.Context, *elasticache.DescribeReplicationGroupsInput, ...func(*elasticache.Options)) (*elasticache.DescribeReplicationGroupsOutput, error) {
	return f.groups, nil
}
func (f redisFixtureClient) DescribeCacheClusters(context.Context, *elasticache.DescribeCacheClustersInput, ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	return f.caches, nil
}

func TestDiscoverRedisBuildsPhysicalNodeRoles(t *testing.T) {
	client := redisFixtureClient{
		groups: &elasticache.DescribeReplicationGroupsOutput{ReplicationGroups: []elasticachetypes.ReplicationGroup{{
			ReplicationGroupId: aws.String("cache"), ConfigurationEndpoint: &elasticachetypes.Endpoint{Address: aws.String("cache.cfg")},
			NodeGroups: []elasticachetypes.NodeGroup{{ReaderEndpoint: &elasticachetypes.Endpoint{Address: aws.String("cache.reader")}, NodeGroupMembers: []elasticachetypes.NodeGroupMember{
				{CacheClusterId: aws.String("cache-001"), CurrentRole: aws.String("primary")},
				{CacheClusterId: aws.String("cache-002"), CurrentRole: aws.String("replica")},
			}}},
		}}},
		caches: &elasticache.DescribeCacheClustersOutput{CacheClusters: []elasticachetypes.CacheCluster{
			{CacheClusterId: aws.String("cache-001"), ReplicationGroupId: aws.String("cache"), CacheNodes: []elasticachetypes.CacheNode{{Endpoint: &elasticachetypes.Endpoint{Address: aws.String("cache-001.node"), Port: aws.Int32(6379)}}}},
			{CacheClusterId: aws.String("cache-002"), ReplicationGroupId: aws.String("cache"), CacheNodes: []elasticachetypes.CacheNode{{Endpoint: &elasticachetypes.Endpoint{Address: aws.String("cache-002.node"), Port: aws.Int32(6379)}}}},
		}},
	}

	items, err := discoverRedis(context.Background(), client, "ap-northeast-1")
	if err != nil {
		t.Fatalf("discoverRedis() error = %v", err)
	}
	if len(items) != 1 || items[0].Endpoint != "cache.cfg" || len(items[0].Nodes) != 2 || items[0].Nodes[0].Role != "primary" || items[0].Nodes[1].Role != "replica" {
		t.Fatalf("discoverRedis() = %#v", items)
	}
}
