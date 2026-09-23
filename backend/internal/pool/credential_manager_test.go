package pool

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newCredentialPoolTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	return db
}

func TestCredentialPoolManagerReusesSameIdentity(t *testing.T) {
	manager := NewCredentialPoolManager()
	key := CredentialPoolKey{ConnectionID: 7, Role: "readonly", Profile: ProfileShadowValidation}
	var opens atomic.Int32
	resolve := func(context.Context) (CredentialPoolDescriptor, error) {
		return CredentialPoolDescriptor{Driver: "mysql", DSN: "readonly:secret@tcp(db:3306)/"}, nil
	}
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		opens.Add(1)
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}

	first, firstStats, err := manager.GetOrCreate(context.Background(), key, resolve, open)
	if err != nil {
		t.Fatalf("first GetOrCreate(): %v", err)
	}
	second, secondStats, err := manager.GetOrCreate(context.Background(), key, resolve, open)
	if err != nil {
		t.Fatalf("second GetOrCreate(): %v", err)
	}
	if first != second || firstStats.Hit || !secondStats.Hit || opens.Load() != 1 {
		t.Fatalf("reuse mismatch: first_hit=%v second_hit=%v opens=%d", firstStats.Hit, secondStats.Hit, opens.Load())
	}
}

func TestCredentialPoolManagerSeparatesRolesAndReplacesChangedCredential(t *testing.T) {
	manager := NewCredentialPoolManager()
	var opens atomic.Int32
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		opens.Add(1)
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}
	resolve := func(dsn string) func(context.Context) (CredentialPoolDescriptor, error) {
		return func(context.Context) (CredentialPoolDescriptor, error) {
			return CredentialPoolDescriptor{Driver: "mysql", DSN: dsn}, nil
		}
	}
	readonly := CredentialPoolKey{ConnectionID: 8, Role: "readonly", Profile: ProfileShadowValidation}
	readwrite := CredentialPoolKey{ConnectionID: 8, Role: "readwrite", Profile: ProfileShadowValidation}

	first, _, _ := manager.GetOrCreate(context.Background(), readonly, resolve("old"), open)
	second, _, _ := manager.GetOrCreate(context.Background(), readwrite, resolve("old"), open)
	replaced, _, _ := manager.GetOrCreate(context.Background(), readonly, resolve("new"), open)
	if first == second || first == replaced || opens.Load() != 3 {
		t.Fatalf("role/fingerprint isolation failed; opens=%d", opens.Load())
	}
}

func TestCredentialPoolManagerCoalescesConcurrentCreation(t *testing.T) {
	manager := NewCredentialPoolManager()
	key := CredentialPoolKey{ConnectionID: 9, Role: "readonly", Profile: ProfileShadowValidation}
	var opens atomic.Int32
	start := make(chan struct{})
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		opens.Add(1)
		<-start
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}
	resolve := func(context.Context) (CredentialPoolDescriptor, error) {
		return CredentialPoolDescriptor{Driver: "mysql", DSN: "same"}, nil
	}

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := manager.GetOrCreate(context.Background(), key, resolve, open); err != nil {
				t.Errorf("GetOrCreate(): %v", err)
			}
		}()
	}
	time.Sleep(10 * time.Millisecond)
	close(start)
	wg.Wait()
	if opens.Load() != 1 {
		t.Fatalf("opens = %d, want 1", opens.Load())
	}
}

func TestCredentialPoolManagerInvalidationDuringCreationRetries(t *testing.T) {
	manager := NewCredentialPoolManager()
	key := CredentialPoolKey{ConnectionID: 10, Role: "readonly", Profile: ProfileShadowValidation}
	var opens atomic.Int32
	started := make(chan struct{})
	resume := make(chan struct{})
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		if opens.Add(1) == 1 {
			close(started)
			<-resume
		}
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}
	resolve := func(context.Context) (CredentialPoolDescriptor, error) {
		return CredentialPoolDescriptor{Driver: "mysql", DSN: "same"}, nil
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := manager.GetOrCreate(context.Background(), key, resolve, open)
		done <- err
	}()
	<-started
	manager.Invalidate(key.ConnectionID)
	close(resume)
	if err := <-done; err != nil {
		t.Fatalf("GetOrCreate(): %v", err)
	}
	if opens.Load() != 2 {
		t.Fatalf("opens = %d, want retry with 2", opens.Load())
	}
}

func TestCredentialPoolManagerDoesNotCacheOpenFailure(t *testing.T) {
	manager := NewCredentialPoolManager()
	key := CredentialPoolKey{ConnectionID: 11, Role: "readonly", Profile: ProfileShadowValidation}
	resolve := func(context.Context) (CredentialPoolDescriptor, error) {
		return CredentialPoolDescriptor{Driver: "mysql", DSN: "same"}, nil
	}
	var opens atomic.Int32
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		if opens.Add(1) == 1 {
			return CredentialPoolOpenResult{}, errors.New("dial failed")
		}
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}
	if _, _, err := manager.GetOrCreate(context.Background(), key, resolve, open); err == nil {
		t.Fatal("first GetOrCreate() error = nil, want dial failure")
	}
	if _, _, err := manager.GetOrCreate(context.Background(), key, resolve, open); err != nil {
		t.Fatalf("second GetOrCreate(): %v", err)
	}
	if opens.Load() != 2 {
		t.Fatalf("opens = %d, want 2", opens.Load())
	}
}

func TestCredentialPoolManagerInvalidatePreventsReuse(t *testing.T) {
	manager := NewCredentialPoolManager()
	key := CredentialPoolKey{ConnectionID: 12, Role: "readonly", Profile: ProfileShadowValidation}
	resolve := func(context.Context) (CredentialPoolDescriptor, error) {
		return CredentialPoolDescriptor{Driver: "mysql", DSN: "same"}, nil
	}
	var opens atomic.Int32
	open := func(context.Context, CredentialPoolDescriptor) (CredentialPoolOpenResult, error) {
		opens.Add(1)
		return CredentialPoolOpenResult{DB: newCredentialPoolTestDB(t)}, nil
	}
	if _, _, err := manager.GetOrCreate(context.Background(), key, resolve, open); err != nil {
		t.Fatalf("first GetOrCreate(): %v", err)
	}
	manager.Invalidate(key.ConnectionID)
	_, stats, err := manager.GetOrCreate(context.Background(), key, resolve, open)
	if err != nil {
		t.Fatalf("second GetOrCreate(): %v", err)
	}
	if stats.Hit || stats.Generation != 1 || opens.Load() != 2 {
		t.Fatalf("post-invalidate stats=%+v opens=%d", stats, opens.Load())
	}
}
