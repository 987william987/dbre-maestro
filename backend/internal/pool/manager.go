package pool

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// InstancePools holds two separate connection pools per target DB instance.
// T7: exec_pool (MaxOpenConns=3) is reserved for ticket execution;
// query_pool (MaxOpenConns=10) is used by the SQL Editor.
// Keeping them separate ensures that a burst of Editor queries never starves
// an in-flight ticket execution (or vice versa).
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
func (m *Manager) GetOrCreate(connID uint64, dsn string) (*InstancePools, error) {
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

	exec, err := openPool(dsn, 3)
	if err != nil {
		return nil, fmt.Errorf("exec_pool open: %w", err)
	}
	query, err := openPool(dsn, 10)
	if err != nil {
		exec.Close()
		return nil, fmt.Errorf("query_pool open: %w", err)
	}

	p := &InstancePools{ExecPool: exec, QueryPool: query}
	m.pools[connID] = p
	return p, nil
}

// Invalidate closes and removes the pools for connID (call after updating a connection).
func (m *Manager) Invalidate(connID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.pools[connID]; ok {
		p.ExecPool.Close()
		p.QueryPool.Close()
		delete(m.pools, connID)
	}
}

func openPool(dsn string, maxOpen int) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(max(1, maxOpen/2))
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(2 * time.Minute)
	return db, nil
}

// BuildMySQLDSN constructs a DSN string from individual fields.
func BuildMySQLDSN(host string, port uint16, user, password, dbName string) string {
	db := ""
	if dbName != "" {
		db = "/" + dbName
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)%s?parseTime=true&charset=utf8mb4&loc=UTC",
		user, password, host, port, db)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
