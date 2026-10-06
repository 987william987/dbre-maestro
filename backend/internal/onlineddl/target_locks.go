package onlineddl

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// TargetLocks prevents platform-started tools from sharing one MySQL server.
// It is process-local because the application currently supports one replica.
type TargetLocks struct {
	mu     sync.Mutex
	active map[string]chan struct{}
}

func NewTargetLocks() *TargetLocks {
	return &TargetLocks{active: make(map[string]chan struct{})}
}

func (l *TargetLocks) TryAcquire(host string, port uint16) (func(), bool) {
	key := targetLockKey(host, port)
	if l == nil || key == ":0" {
		return nil, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.active[key]; exists {
		return nil, false
	}
	done := make(chan struct{})
	l.active[key] = done
	return l.release(key, done), true
}

func (l *TargetLocks) Acquire(ctx context.Context, host string, port uint16) (func(), error) {
	key := targetLockKey(host, port)
	if l == nil || key == ":0" {
		return nil, ErrInvalidParameters
	}
	for {
		l.mu.Lock()
		done, exists := l.active[key]
		if !exists {
			done = make(chan struct{})
			l.active[key] = done
			l.mu.Unlock()
			return l.release(key, done), nil
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
	}
}

func targetLockKey(host string, port uint16) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) + ":" + strconv.Itoa(int(port))
}

func (l *TargetLocks) release(key string, done chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			if l.active[key] == done {
				delete(l.active, key)
				close(done)
			}
			l.mu.Unlock()
		})
	}
}
