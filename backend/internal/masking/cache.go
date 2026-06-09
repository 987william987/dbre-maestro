package masking

import (
	"sync"
	"time"
)

const cacheTTL = 5 * time.Minute

// RuleCache is an in-process TTL cache for masking rules.
// TE2: TTL ensures stale rules are evicted even if active invalidation is missed.
// Active invalidation happens when a DBA saves new rules (call Invalidate).
type RuleCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry // key = "db_connection_id:table"
}

type cacheEntry struct {
	rules     []Rule
	expiresAt time.Time
}

var globalCache = &RuleCache{entries: make(map[string]*cacheEntry)}

func GlobalCache() *RuleCache { return globalCache }

func (c *RuleCache) Get(key string) ([]Rule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.rules, true
}

func (c *RuleCache) Set(key string, rules []Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{
		rules:     rules,
		expiresAt: time.Now().Add(cacheTTL),
	}
}

// Invalidate removes all cached entries (called when DBA saves new masking rules).
func (c *RuleCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
}

// InvalidateTable removes cached entries for a specific table key.
func (c *RuleCache) InvalidateTable(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
