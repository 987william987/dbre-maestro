package sessionmanagement

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

type fakeLoopStore struct {
	mu              sync.Mutex
	job             model.SessionLoopJob
	finishStatus    string
	interruptCount  int64
	interruptCalled bool
}

func (s *fakeLoopStore) ListPending(context.Context, uint) ([]model.SessionLoopJob, error) {
	return nil, nil
}
func (s *fakeLoopStore) GetByID(context.Context, uint64) (*model.SessionLoopJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := s.job
	return &copy, nil
}
func (s *fakeLoopStore) Claim(context.Context, uint64) (bool, error) { return true, nil }
func (s *fakeLoopStore) InterruptRunning(context.Context) (int64, error) {
	s.interruptCalled = true
	return s.interruptCount, nil
}
func (s *fakeLoopStore) Finish(_ context.Context, _ uint64, status, _, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishStatus, s.job.Status = status, status
	s.job.PrefixEncrypted = nil
	return true, nil
}
func (s *fakeLoopStore) AddKills(_ context.Context, _ uint64, count uint) (*model.SessionLoopJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.KillCount += count
	s.job.ConsecutiveErrors = 0
	copy := s.job
	return &copy, nil
}
func (s *fakeLoopStore) RecordError(_ context.Context, _ uint64, kills uint, _, _ string) (*model.SessionLoopJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.KillCount += kills
	s.job.ConsecutiveErrors++
	copy := s.job
	return &copy, nil
}
func (s *fakeLoopStore) DecryptPrefix(*model.SessionLoopJob) (string, error) {
	return "select * from orders", nil
}

func runningLoopJob() model.SessionLoopJob {
	expires := time.Now().Add(time.Hour)
	return model.SessionLoopJob{ID: 1, Status: model.SessionLoopStatusRunning, IntervalSeconds: 1, MaxKills: 100, ExpiresAt: &expires, PrefixEncrypted: []byte("encrypted")}
}

func TestLoopWorkerStopsAfterThreeConsecutiveIterationErrors(t *testing.T) {
	store := &fakeLoopStore{job: runningLoopJob()}
	worker := NewLoopWorker(store, nil, nil, nil, nil, nil)
	worker.iterate = func(context.Context, *model.SessionLoopJob, string) (uint, error) { return 0, errors.New("offline") }
	worker.waitNext = func(context.Context, uint64, time.Duration) bool { return true }
	worker.execute(context.Background(), 1)
	if store.finishStatus != model.SessionLoopStatusFailed || store.job.ConsecutiveErrors != 3 || len(store.job.PrefixEncrypted) != 0 {
		t.Fatalf("job = %#v, finish = %q", store.job, store.finishStatus)
	}
}

func TestLoopWorkerStopsAtKillLimit(t *testing.T) {
	store := &fakeLoopStore{job: runningLoopJob()}
	store.job.MaxKills = 2
	worker := NewLoopWorker(store, nil, nil, nil, nil, nil)
	worker.iterate = func(context.Context, *model.SessionLoopJob, string) (uint, error) { return 2, nil }
	worker.waitNext = func(context.Context, uint64, time.Duration) bool {
		t.Fatal("must not wait after reaching kill limit")
		return false
	}
	worker.execute(context.Background(), 1)
	if store.finishStatus != model.SessionLoopStatusLimitReached || store.job.KillCount != 2 {
		t.Fatalf("job = %#v", store.job)
	}
}

func TestLoopWorkerExpiresWithoutRunningAnotherIteration(t *testing.T) {
	store := &fakeLoopStore{job: runningLoopJob()}
	expired := time.Now().Add(-time.Second)
	store.job.ExpiresAt = &expired
	worker := NewLoopWorker(store, nil, nil, nil, nil, nil)
	worker.iterate = func(context.Context, *model.SessionLoopJob, string) (uint, error) {
		t.Fatal("expired job must not run")
		return 0, nil
	}
	worker.execute(context.Background(), 1)
	if store.finishStatus != model.SessionLoopStatusExpired {
		t.Fatalf("finish = %q", store.finishStatus)
	}
}

func TestLoopWorkerHonorsStoppedState(t *testing.T) {
	job := runningLoopJob()
	job.Status = model.SessionLoopStatusStopped
	store := &fakeLoopStore{job: job}
	worker := NewLoopWorker(store, nil, nil, nil, nil, nil)
	worker.iterate = func(context.Context, *model.SessionLoopJob, string) (uint, error) {
		t.Fatal("stopped job must not run")
		return 0, nil
	}
	worker.execute(context.Background(), 1)
	if store.finishStatus != "" {
		t.Fatalf("unexpected finish = %q", store.finishStatus)
	}
}

func TestLoopWorkerInterruptsRunningJobsOnStartup(t *testing.T) {
	store := &fakeLoopStore{job: runningLoopJob(), interruptCount: 1}
	worker := NewLoopWorker(store, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Start(ctx)
	if !store.interruptCalled {
		t.Fatal("startup must mark leftover running jobs interrupted")
	}
}
