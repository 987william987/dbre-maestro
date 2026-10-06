package tableschema

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type workerStoreStub struct {
	queued     []model.TableSchemaSyncJob
	claimed    bool
	claimID    uint64
	interrupts int
	current    *model.TableSchemaSyncJob
}

func (s *workerStoreStub) ListQueued(context.Context, uint) ([]model.TableSchemaSyncJob, error) {
	return s.queued, nil
}
func (s *workerStoreStub) GetByID(context.Context, uint64) (*model.TableSchemaSyncJob, error) {
	return s.current, nil
}
func (s *workerStoreStub) ListItems(context.Context, uint64) ([]model.TableSchemaSyncJobItem, error) {
	return nil, nil
}
func (s *workerStoreStub) Claim(_ context.Context, id uint64) (bool, error) {
	s.claimID = id
	return s.claimed, nil
}
func (s *workerStoreStub) InterruptRunning(context.Context) (int64, error) {
	s.interrupts++
	return 0, nil
}
func (s *workerStoreStub) MarkItemStarted(context.Context, uint64) (bool, error) { return true, nil }
func (s *workerStoreStub) MarkItemCreated(context.Context, uint64, string, time.Duration) (bool, error) {
	return true, nil
}
func (s *workerStoreStub) Fail(context.Context, uint64, uint64, string, time.Duration) error {
	return nil
}
func (s *workerStoreStub) Complete(context.Context, uint64) (bool, error)      { return true, nil }
func (s *workerStoreStub) MarkCancelled(context.Context, uint64) (bool, error) { return true, nil }

func TestSyncWorkerDispatchRunsOnlyClaimedJob(t *testing.T) {
	store := &workerStoreStub{queued: []model.TableSchemaSyncJob{{ID: 42}}, claimed: true, current: &model.TableSchemaSyncJob{ID: 42, Status: model.TableSchemaSyncCompleted}}
	worker := &SyncWorker{jobs: store, logger: slog.Default()}
	runs := 0
	worker.run = func(_ context.Context, job *model.TableSchemaSyncJob) error {
		runs++
		if job.ID != 42 {
			t.Fatalf("job id = %d", job.ID)
		}
		return nil
	}
	worker.dispatch(context.Background())
	if store.claimID != 42 || runs != 1 {
		t.Fatalf("claim = %d, runs = %d", store.claimID, runs)
	}

	store.claimed = false
	worker.dispatch(context.Background())
	if runs != 1 {
		t.Fatalf("unclaimed job ran; runs = %d", runs)
	}
}

func TestSyncWorkerStartInterruptsOldJobsBeforePolling(t *testing.T) {
	store := &workerStoreStub{}
	worker := &SyncWorker{jobs: store, logger: slog.Default(), poll: time.Hour, run: func(context.Context, *model.TableSchemaSyncJob) error { return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Start(ctx)
	if store.interrupts != 1 {
		t.Fatalf("interrupt calls = %d", store.interrupts)
	}
}

func TestSyncWorkerTerminalAuditUsesPersistedStatusAndCannotFailJob(t *testing.T) {
	store := &workerStoreStub{current: &model.TableSchemaSyncJob{ID: 42, RequestedBy: 7, Status: model.TableSchemaSyncCancelled, CreatedCount: 1, NotStartedCount: 2}}
	worker := &SyncWorker{jobs: store, logger: slog.Default()}
	called := 0
	worker.audit = func(_ context.Context, entry repository.AuditEntry) error {
		called++
		details := entry.Details.(map[string]any)
		if details["status"] != model.TableSchemaSyncCancelled {
			t.Fatalf("audit status = %v", details["status"])
		}
		return errors.New("audit unavailable")
	}
	worker.recordTerminal(context.Background(), store.current, time.Second, nil)
	if called != 1 {
		t.Fatalf("audit calls = %d", called)
	}
}
