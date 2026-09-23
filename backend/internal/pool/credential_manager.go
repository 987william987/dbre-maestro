package pool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var errCredentialPoolInvalidated = errors.New("credential pool invalidated during creation")

type CredentialPoolKey struct {
	ConnectionID uint64
	Role         string
	Profile      Profile
}

type CredentialPoolDescriptor struct {
	Driver string
	DSN    string
}

type CredentialPoolOpenResult struct {
	DB           *sql.DB
	PingDuration time.Duration
}

type CredentialPoolStats struct {
	Hit          bool
	WaitDuration time.Duration
	OpenDuration time.Duration
	PingDuration time.Duration
	Generation   uint64
}

type credentialPoolEntry struct {
	fingerprint [sha256.Size]byte
	db          *sql.DB
}

type credentialPoolAcquireResult struct {
	db           *sql.DB
	hit          bool
	openDuration time.Duration
	pingDuration time.Duration
	generation   uint64
}

type CredentialPoolManager struct {
	mu          sync.Mutex
	entries     map[CredentialPoolKey]credentialPoolEntry
	generations map[uint64]uint64
	creates     singleflight.Group
}

func NewCredentialPoolManager() *CredentialPoolManager {
	return &CredentialPoolManager{
		entries:     make(map[CredentialPoolKey]credentialPoolEntry),
		generations: make(map[uint64]uint64),
	}
}

var shadowValidationPools = NewCredentialPoolManager()

func ShadowValidationPools() *CredentialPoolManager { return shadowValidationPools }

func (m *CredentialPoolManager) GetOrCreate(
	ctx context.Context,
	key CredentialPoolKey,
	resolve func(context.Context) (CredentialPoolDescriptor, error),
	open func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error),
) (*sql.DB, CredentialPoolStats, error) {
	if key.ConnectionID == 0 || key.Role == "" {
		return nil, CredentialPoolStats{}, errors.New("credential pool key requires connection id and role")
	}

	waitStartedAt := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		m.mu.Lock()
		generation := m.generations[key.ConnectionID]
		m.mu.Unlock()

		descriptor, err := resolve(ctx)
		if err != nil {
			return nil, CredentialPoolStats{}, err
		}
		fingerprint := sha256.Sum256([]byte(descriptor.Driver + "\x00" + descriptor.DSN))
		flightKey := fmt.Sprintf("%d:%s:%s:%d:%x", key.ConnectionID, key.Role, key.Profile, generation, fingerprint)

		resultCh := m.creates.DoChan(flightKey, func() (any, error) {
			m.mu.Lock()
			if m.generations[key.ConnectionID] != generation {
				m.mu.Unlock()
				return nil, errCredentialPoolInvalidated
			}
			if entry, ok := m.entries[key]; ok && entry.fingerprint == fingerprint {
				m.mu.Unlock()
				return credentialPoolAcquireResult{db: entry.db, hit: true, generation: generation}, nil
			}
			m.mu.Unlock()

			openStartedAt := time.Now()
			opened, err := open(context.WithoutCancel(ctx), descriptor)
			openDuration := time.Since(openStartedAt)
			if err != nil {
				return nil, err
			}
			if opened.DB == nil {
				return nil, errors.New("credential pool opener returned nil database")
			}

			m.mu.Lock()
			if m.generations[key.ConnectionID] != generation {
				m.mu.Unlock()
				_ = opened.DB.Close()
				return nil, errCredentialPoolInvalidated
			}
			previous := m.entries[key]
			m.entries[key] = credentialPoolEntry{fingerprint: fingerprint, db: opened.DB}
			m.mu.Unlock()

			if previous.db != nil && previous.db != opened.DB {
				_ = previous.db.Close()
			}
			return credentialPoolAcquireResult{
				db: opened.DB, openDuration: openDuration,
				pingDuration: opened.PingDuration, generation: generation,
			}, nil
		})

		select {
		case <-ctx.Done():
			return nil, CredentialPoolStats{}, ctx.Err()
		case result := <-resultCh:
			if errors.Is(result.Err, errCredentialPoolInvalidated) {
				continue
			}
			if result.Err != nil {
				return nil, CredentialPoolStats{}, result.Err
			}
			acquired := result.Val.(credentialPoolAcquireResult)
			return acquired.db, CredentialPoolStats{
				Hit: acquired.hit, WaitDuration: time.Since(waitStartedAt),
				OpenDuration: acquired.openDuration, PingDuration: acquired.pingDuration,
				Generation: acquired.generation,
			}, nil
		}
	}

	return nil, CredentialPoolStats{}, errCredentialPoolInvalidated
}

func (m *CredentialPoolManager) Invalidate(connectionID uint64) {
	var closing []*sql.DB
	m.mu.Lock()
	m.generations[connectionID]++
	for key, entry := range m.entries {
		if key.ConnectionID == connectionID {
			closing = append(closing, entry.db)
			delete(m.entries, key)
		}
	}
	m.mu.Unlock()
	for _, db := range closing {
		_ = db.Close()
	}
}
