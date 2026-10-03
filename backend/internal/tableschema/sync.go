package tableschema

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxSyncPreviews = 1000

type syncPreview struct {
	ActorID     uint64
	Fingerprint string
	ExpiresAt   time.Time
	Used        bool
}
type SyncPreviewStore struct {
	mu      sync.Mutex
	entries map[string]syncPreview
	now     func() time.Time
}

func NewSyncPreviewStore() *SyncPreviewStore {
	return &SyncPreviewStore{entries: map[string]syncPreview{}, now: time.Now}
}

func (s *SyncPreviewStore) Issue(actorID uint64, fingerprint string) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := s.now().Add(PreviewTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if len(s.entries) >= maxSyncPreviews {
		oldest := ""
		for key, item := range s.entries {
			if oldest == "" || item.ExpiresAt.Before(s.entries[oldest].ExpiresAt) {
				oldest = key
			}
		}
		delete(s.entries, oldest)
	}
	s.entries[token] = syncPreview{ActorID: actorID, Fingerprint: fingerprint, ExpiresAt: expires}
	return token, expires, nil
}

func (s *SyncPreviewStore) Consume(token string, actorID uint64, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	item, ok := s.entries[token]
	if !ok {
		return &StableError{Code: ErrorPreviewExpired, Message: "sync preview token is missing or expired"}
	}
	if item.Used {
		return &StableError{Code: ErrorPreviewReplayed, Message: "sync preview token was already used"}
	}
	item.Used = true
	s.entries[token] = item
	if item.ActorID != actorID {
		return &StableError{Code: ErrorPreviewReplayed, Message: "sync preview token does not belong to this actor"}
	}
	if item.Fingerprint != fingerprint {
		stored, current := strings.Split(item.Fingerprint, ":"), strings.Split(fingerprint, ":")
		if len(stored) == 4 && len(current) == 4 {
			if stored[0] != current[0] {
				return &StableError{Code: ErrorPreviewReplayed, Message: "sync preview request does not match"}
			}
			if stored[3] != current[3] {
				return &StableError{Code: ErrorTargetCapabilityChanged, Message: "target capabilities changed; create a new preview"}
			}
		}
		return &StableError{Code: ErrorSourceDDLChanged, Message: "source schema or dependencies changed; create a new preview"}
	}
	return nil
}
func (s *SyncPreviewStore) prune() {
	now := s.now()
	for key, item := range s.entries {
		if !now.Before(item.ExpiresAt) {
			delete(s.entries, key)
		}
	}
}

func BuildSyncPreview(ctx context.Context, source, target Queryer, request SyncPreviewRequest) (*ExportResult, string, error) {
	snapshot, err := LoadSnapshot(ctx, source, request.Source.Database, request.Source.Tables)
	if err != nil {
		return nil, "", err
	}
	deps, err := LoadDependencies(ctx, source, request.Source.Database, request.Source.Tables)
	if err != nil {
		return nil, "", err
	}
	analysis, err := AnalyzeDependencies(request.Source.Database, request.Source.Tables, deps)
	if err != nil {
		return nil, "", err
	}
	if len(analysis.Cycles) > 0 {
		return nil, "", &StableError{Code: ErrorForeignKeyCycle, Message: "selected tables contain a foreign key cycle"}
	}
	caps, err := LoadCapabilities(ctx, target)
	if err != nil {
		return nil, "", err
	}
	if err := preflightTarget(ctx, target, request, analysis); err != nil {
		return nil, "", err
	}
	result, err := assembleExport(ctx, snapshot, request.Target.Database, analysis, request.Transformation, caps)
	if err != nil {
		return nil, "", err
	}
	fingerprint, err := syncFingerprint(request, snapshot, deps, caps)
	return result, fingerprint, err
}

func preflightTarget(ctx context.Context, target Queryer, request SyncPreviewRequest, analysis DependencyAnalysis) error {
	for _, table := range analysis.Order {
		exists, err := tableExists(ctx, target, request.Target.Database, table)
		if err != nil {
			return err
		}
		if exists {
			return &StableError{Code: ErrorTargetTableExists, Message: "one or more target tables already exist"}
		}
	}
	for _, dep := range analysis.External {
		db := dep.ReferencedDatabase
		if db == request.Source.Database {
			db = request.Target.Database
		}
		exists, err := tableExists(ctx, target, db, dep.ReferencedTable)
		if err != nil {
			return err
		}
		if !exists {
			return &StableError{Code: ErrorExternalMissing, Message: "an external foreign key dependency is missing on target"}
		}
	}
	return nil
}

func RevalidateSyncPreview(ctx context.Context, store *SyncPreviewStore, token string, actorID uint64, source, target Queryer, request SyncPreviewRequest) (*ExportResult, error) {
	result, fingerprint, err := BuildSyncPreview(ctx, source, target, request)
	if err != nil {
		return nil, err
	}
	if err := store.Consume(token, actorID, fingerprint); err != nil {
		return nil, err
	}
	return result, nil
}

func tableExists(ctx context.Context, db Queryer, database, table string) (bool, error) {
	if validateIdentifier(database) != nil || validateIdentifier(table) != nil {
		return false, &StableError{Code: ErrorInvalidTableSelection, Message: "invalid target identifier"}
	}
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND TABLE_TYPE = 'BASE TABLE')`, database, table).Scan(&exists)
	return exists, err
}

func syncFingerprint(request SyncPreviewRequest, snapshot *Snapshot, deps []Dependency, caps Capabilities) (string, error) {
	sort.Slice(deps, func(i, j int) bool { return dependencyKey(deps[i]) < dependencyKey(deps[j]) })
	parts := []any{request, snapshot, deps, caps}
	hashes := make([]string, len(parts))
	for i, part := range parts {
		raw, err := json.Marshal(part)
		if err != nil {
			return "", fmt.Errorf("marshal sync fingerprint: %w", err)
		}
		sum := sha256.Sum256(raw)
		hashes[i] = hex.EncodeToString(sum[:])
	}
	return strings.Join(hashes, ":"), nil
}
