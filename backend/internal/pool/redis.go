package pool

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisSession interface {
	Do(ctx context.Context, args ...interface{}) *redis.Cmd
	Close() error
}

var newRedisSession = func(client *redis.Client) redisSession {
	return client.Conn()
}

type RedisConnOptions struct {
	ConnID   uint64
	Host     string
	Port     uint16
	Username string
	Password string
	DB       int
	SSLMode  string
}

type redisClientVariant struct {
	cacheKey string
	useTLS   bool
	cluster  bool
}

// RedisManager holds one Redis client per target Redis instance and mode.
type RedisManager struct {
	mu      sync.RWMutex
	clients map[string]redis.UniversalClient
}

var redisGlobal = &RedisManager{clients: make(map[string]redis.UniversalClient)}

func RedisGlobal() *RedisManager { return redisGlobal }

func (m *RedisManager) Ping(ctx context.Context, options RedisConnOptions) error {
	addr := BuildRedisAddr(options.Host, options.Port)
	var lastErr error
	for _, variant := range buildRedisClientVariants(options) {
		client := m.getOrCreate(options, addr, variant)
		if err := client.Ping(ctx).Err(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (m *RedisManager) Do(ctx context.Context, options RedisConnOptions, args ...interface{}) (interface{}, error) {
	addr := BuildRedisAddr(options.Host, options.Port)
	var lastErr error
	for _, variant := range buildRedisClientVariants(options) {
		client := m.getOrCreate(options, addr, variant)
		result, err := client.Do(ctx, args...).Result()
		if err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	return nil, lastErr
}

func (m *RedisManager) DoInDB(ctx context.Context, options RedisConnOptions, args ...interface{}) (interface{}, error) {
	addr := BuildRedisAddr(options.Host, options.Port)
	var lastErr error
	for _, variant := range buildRedisClientVariants(options) {
		if variant.cluster {
			if options.DB != 0 {
				lastErr = fmt.Errorf("redis cluster only supports db 0")
				continue
			}
			client := m.getOrCreate(options, addr, variant)
			result, err := client.Do(ctx, args...).Result()
			if err != nil {
				lastErr = err
				continue
			}
			return result, nil
		}

		client := m.getOrCreate(options, addr, variant)
		standalone, ok := client.(*redis.Client)
		if !ok {
			lastErr = fmt.Errorf("unexpected redis client type")
			continue
		}

		conn := newRedisSession(standalone)
		if options.DB != 0 {
			if err := conn.Do(ctx, "SELECT", options.DB).Err(); err != nil {
				_ = conn.Close()
				lastErr = err
				continue
			}
		}

		result, err := conn.Do(ctx, args...).Result()
		_ = conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	return nil, lastErr
}

func (m *RedisManager) getOrCreate(options RedisConnOptions, addr string, variant redisClientVariant) redis.UniversalClient {
	m.mu.RLock()
	if c, ok := m.clients[variant.cacheKey]; ok {
		m.mu.RUnlock()
		return c
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[variant.cacheKey]; ok {
		return c
	}

	var tlsConfig *tls.Config
	if variant.useTLS {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	var c redis.UniversalClient
	if variant.cluster {
		c = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        []string{addr},
			Username:     options.Username,
			Password:     options.Password,
			DialTimeout:  2 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			MaxRedirects: 3,
			TLSConfig:    tlsConfig,
		})
	} else {
		c = redis.NewClient(&redis.Options{
			Addr:         addr,
			Username:     options.Username,
			Password:     options.Password,
			DB:           0,
			DialTimeout:  2 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			TLSConfig:    tlsConfig,
		})
	}

	m.clients[variant.cacheKey] = c
	return c
}

func (m *RedisManager) Invalidate(connID uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	connPrefix := fmt.Sprintf("%d|", connID)
	for key, c := range m.clients {
		if strings.HasPrefix(key, connPrefix) {
			c.Close()
			delete(m.clients, key)
		}
	}
}

func BuildRedisAddr(host string, port uint16) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func buildRedisClientVariants(options RedisConnOptions) []redisClientVariant {
	cluster := isRedisClusterEndpoint(options.Host)
	baseKey := fmt.Sprintf("%d|%t|", options.ConnID, cluster)

	switch strings.ToLower(strings.TrimSpace(options.SSLMode)) {
	case "require":
		return []redisClientVariant{{cacheKey: baseKey + "tls", useTLS: true, cluster: cluster}}
	case "disable":
		return []redisClientVariant{{cacheKey: baseKey + "plain", useTLS: false, cluster: cluster}}
	default:
		return []redisClientVariant{
			{cacheKey: baseKey + "plain", useTLS: false, cluster: cluster},
			{cacheKey: baseKey + "tls", useTLS: true, cluster: cluster},
		}
	}
}

func isRedisClusterEndpoint(host string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(host)), "clustercfg.")
}
