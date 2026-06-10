package pool

import (
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RedisManager holds one *redis.Client per target Redis instance.
type RedisManager struct {
	mu      sync.RWMutex
	clients map[uint64]*redis.Client
}

var redisGlobal = &RedisManager{clients: make(map[uint64]*redis.Client)}

func RedisGlobal() *RedisManager { return redisGlobal }

// GetOrCreate returns an existing client or opens a new one.
func (m *RedisManager) GetOrCreate(connID uint64, addr, password string, db int) *redis.Client {
	m.mu.RLock()
	if c, ok := m.clients[connID]; ok {
		m.mu.RUnlock()
		return c
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[connID]; ok {
		return c
	}
	c := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	m.clients[connID] = c
	return c
}

// Invalidate closes and removes the client for connID.
func (m *RedisManager) Invalidate(connID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[connID]; ok {
		c.Close()
		delete(m.clients, connID)
	}
}

// BuildRedisAddr returns "host:port" for a Redis connection.
func BuildRedisAddr(host string, port uint16) string {
	return fmt.Sprintf("%s:%d", host, port)
}
