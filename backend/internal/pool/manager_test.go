package pool

import "testing"

func TestBuildMySQLDSNAllowsEmptyDatabaseName(t *testing.T) {
	got := BuildMySQLDSN("db.internal", 3306, "readonly", "secret", "")
	want := "readonly:secret@tcp(db.internal:3306)/?parseTime=true&charset=utf8mb4&loc=UTC&columnsWithAlias=true"
	if got != want {
		t.Fatalf("dsn = %q, want %q", got, want)
	}
}

func TestBuildMySQLDSNWithDatabaseName(t *testing.T) {
	got := BuildMySQLDSN("db.internal", 3306, "readonly", "secret", "analytics")
	want := "readonly:secret@tcp(db.internal:3306)/analytics?parseTime=true&charset=utf8mb4&loc=UTC&columnsWithAlias=true"
	if got != want {
		t.Fatalf("dsn = %q, want %q", got, want)
	}
}

func TestBuildPostgresDSNDefaultsDatabaseNameToPostgres(t *testing.T) {
	got := BuildPostgresDSN("pg.internal", 5432, "postgres", "secret", nil, "prefer")
	want := "postgres://postgres:secret@pg.internal:5432/postgres?sslmode=prefer"
	if got != want {
		t.Fatalf("dsn = %q, want %q", got, want)
	}
}

func TestBuildRedisClientVariantsPreferTriesPlainThenTLS(t *testing.T) {
	variants := buildRedisClientVariants(RedisConnOptions{
		ConnID:  9,
		Host:    "clustercfg.example.cache.amazonaws.com",
		SSLMode: "prefer",
	})

	if len(variants) != 2 {
		t.Fatalf("len(variants) = %d, want 2", len(variants))
	}
	if variants[0].useTLS || !variants[1].useTLS {
		t.Fatalf("variants = %#v, want plain then tls", variants)
	}
	if !variants[0].cluster || !variants[1].cluster {
		t.Fatalf("variants = %#v, want cluster=true", variants)
	}
}

func TestBuildRedisClientVariantsRequireUsesOnlyTLS(t *testing.T) {
	variants := buildRedisClientVariants(RedisConnOptions{
		ConnID:  9,
		Host:    "redis.internal",
		SSLMode: "require",
	})

	if len(variants) != 1 {
		t.Fatalf("len(variants) = %d, want 1", len(variants))
	}
	if !variants[0].useTLS {
		t.Fatalf("variants[0].useTLS = %v, want true", variants[0].useTLS)
	}
	if variants[0].cluster {
		t.Fatalf("variants[0].cluster = %v, want false", variants[0].cluster)
	}
}

func TestIsRedisClusterEndpoint(t *testing.T) {
	if !isRedisClusterEndpoint("clustercfg.aws-jp-edgex-share-redis-nonprod.vobmfe.apne1.cache.amazonaws.com") {
		t.Fatalf("expected cluster endpoint to be detected")
	}
	if isRedisClusterEndpoint("master.redis.internal") {
		t.Fatalf("expected non-cluster endpoint to remain false")
	}
}
