package handler

import (
	"sync"
	"time"
)

type requestRateLimiter interface {
	Allow(key string, now time.Time) bool
}

func newRequestRateLimiter(limit int, window time.Duration) requestRateLimiter {
	return newInMemoryRequestRateLimiter(limit, window)
}

func newInMemoryRequestRateLimiter(limit int, window time.Duration) requestRateLimiter {
	return &inMemoryRequestRateLimiter{
		limit:   limit,
		window:  window,
		history: make(map[string][]time.Time),
	}
}

type inMemoryRequestRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	history map[string][]time.Time
}

func (l *inMemoryRequestRateLimiter) Allow(key string, now time.Time) bool {
	if l == nil || key == "" || l.limit <= 0 || l.window <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	current := l.history[key]
	filtered := current[:0]
	for _, hitAt := range current {
		if hitAt.After(cutoff) {
			filtered = append(filtered, hitAt)
		}
	}
	if len(filtered) >= l.limit {
		l.history[key] = append([]time.Time(nil), filtered...)
		return false
	}

	filtered = append(filtered, now)
	l.history[key] = append([]time.Time(nil), filtered...)
	return true
}
