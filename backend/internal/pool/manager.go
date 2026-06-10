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

// InstancePools holds two separate connection pools per target DB instance.
// T7: exec_pool (MaxOpenConns=3) is reserved for ticket execution;
// query_pool (MaxOpenConns=10) is used by the SQL Editor.
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

	exec, err := openPool(driver, dsn, 3)
	if err != nil {
		return nil, fmt.Errorf("exec_pool open: %w", err)
	}
	query, err := openPool(driver, dsn, 10)
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

func openPool(driver, dsn string, maxOpen int) (*sql.DB, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(max(1, maxOpen/2))
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)
	return db, nil
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
	db := ""
	if dbName != "" {
		db = "/" + dbName
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)%s?parseTime=true&charset=utf8mb4&loc=UTC",
		user, password, host, port, db)
}

// BuildPostgresDSN constructs a PostgreSQL connection URL for pgx/v5/stdlib.
func BuildPostgresDSN(host string, port uint16, user, password string, dbName *string, sslMode string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	if dbName != nil && *dbName != "" {
		u.Path = "/" + *dbName
	}
	q := url.Values{}
	if sslMode != "" {
		q.Set("sslmode", sslMode)
	} else {
		q.Set("sslmode", "prefer")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
