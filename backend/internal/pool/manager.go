package pool

import (
	"database/sql"
	"fmt"
	"net/url"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/dbre-maestro/maestro/internal/model"
)

type Profile string

const (
	ProfileQuery            Profile = "query"
	ProfileExec             Profile = "exec"
	ProfileMetadata         Profile = "metadata"
	ProfileScopedPGQuery    Profile = "scoped_pg_query"
	ProfileShadowValidation Profile = "shadow_validation"
)

type ProfileConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

var defaultProfileConfigs = map[Profile]ProfileConfig{
	ProfileQuery: {
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	},
	ProfileExec: {
		MaxOpenConns:    3,
		MaxIdleConns:    1,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	},
	ProfileMetadata: {
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: 2 * time.Minute,
		ConnMaxIdleTime: 1 * time.Minute,
	},
	ProfileScopedPGQuery: {
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: 2 * time.Minute,
		ConnMaxIdleTime: 1 * time.Minute,
	},
	ProfileShadowValidation: {
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: 2 * time.Minute,
		ConnMaxIdleTime: 1 * time.Minute,
	},
}

var profileConfigs = cloneProfileConfigs(defaultProfileConfigs)

func cloneProfileConfigs(source map[Profile]ProfileConfig) map[Profile]ProfileConfig {
	cloned := make(map[Profile]ProfileConfig, len(source))
	for profile, config := range source {
		cloned[profile] = config
	}
	return cloned
}

func DefaultConfigForProfile(profile Profile) ProfileConfig {
	config, ok := defaultProfileConfigs[profile]
	if !ok {
		return defaultProfileConfigs[ProfileQuery]
	}
	return config
}

func ConfigForProfile(profile Profile) ProfileConfig {
	config, ok := profileConfigs[profile]
	if !ok {
		return profileConfigs[ProfileQuery]
	}
	return config
}

func SetProfileConfigs(configs map[Profile]ProfileConfig) {
	if len(configs) == 0 {
		profileConfigs = cloneProfileConfigs(defaultProfileConfigs)
		return
	}
	next := cloneProfileConfigs(defaultProfileConfigs)
	for profile, config := range configs {
		next[profile] = config
	}
	profileConfigs = next
}

// InstancePools holds two separate connection pools per target DB instance.
// exec_pool is reserved for ticket execution; query_pool is used by interactive read paths.
type InstancePools struct {
	ExecPool  *sql.DB
	QueryPool *sql.DB
}

type Manager struct {
	mu    sync.RWMutex
	pools map[uint64]*InstancePools
}

var global = &Manager{pools: make(map[uint64]*InstancePools)}

func Global() *Manager { return global }

// GetOrCreate returns existing pools or creates new ones for connID.
// driver must be "mysql" or "pgx".
func (m *Manager) GetOrCreate(connID uint64, driver, dsn string) (*InstancePools, error) {
	m.mu.RLock()
	if p, ok := m.pools[connID]; ok {
		m.mu.RUnlock()
		return p, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[connID]; ok {
		return p, nil
	}

	exec, err := Open(driver, dsn, ProfileExec)
	if err != nil {
		return nil, fmt.Errorf("exec_pool open: %w", err)
	}
	query, err := Open(driver, dsn, ProfileQuery)
	if err != nil {
		exec.Close()
		return nil, fmt.Errorf("query_pool open: %w", err)
	}

	p := &InstancePools{ExecPool: exec, QueryPool: query}
	m.pools[connID] = p
	return p, nil
}

// Invalidate closes and removes the pools for connID.
func (m *Manager) Invalidate(connID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[connID]; ok {
		p.ExecPool.Close()
		p.QueryPool.Close()
		delete(m.pools, connID)
	}
}

func Open(driver, dsn string, profile Profile) (*sql.DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	ApplyProfile(db, profile)
	return db, nil
}

func ApplyProfile(db *sql.DB, profile Profile) {
	config := ConfigForProfile(profile)
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)
}

// BuildDSN returns the driver name and DSN for a connection.
// Redis connections are not handled here; use RedisGlobal() instead.
func BuildDSN(conn *model.DBConnection, password string) (driver, dsn string) {
	switch conn.DBType {
	case "postgres", "postgresql":
		return "pgx", BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, conn.DatabaseName, conn.SSLMode)
	default: // mysql
		dbName := ""
		if conn.DatabaseName != nil {
			dbName = *conn.DatabaseName
		}
		return "mysql", BuildMySQLDSN(conn.Host, conn.Port, conn.Username, password, dbName)
	}
}

// BuildMySQLDSN constructs a MySQL DSN string.
func BuildMySQLDSN(host string, port uint16, user, password, dbName string) string {
	db := "/"
	if dbName != "" {
		db = "/" + dbName
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)%s?parseTime=true&charset=utf8mb4&loc=UTC&columnsWithAlias=true",
		user, password, host, port, db)
}

// BuildPostgresDSN constructs a PostgreSQL connection URL for pgx/v5/stdlib.
func BuildPostgresDSN(host string, port uint16, user, password string, dbName *string, sslMode string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	targetDatabase := "postgres"
	if dbName != nil && *dbName != "" {
		targetDatabase = *dbName
	}
	u.Path = "/" + targetDatabase
	q := url.Values{}
	if sslMode != "" {
		q.Set("sslmode", sslMode)
	} else {
		q.Set("sslmode", "prefer")
	}
	u.RawQuery = q.Encode()
	return u.String()
}
