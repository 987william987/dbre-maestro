package binlogexport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/my2sql"
	"github.com/dbre-maestro/maestro/internal/repository"
)

func TestBuildRequestKeepsTimeSnapshotAndTimezone(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	database := "app"
	endFile := "mysql-bin.000003"
	endPos := uint64(4567)
	job := &model.MySQLBinlogExportJob{RangeMode: "time", Timezone: "Asia/Taipei", RequestedStartTime: &start,
		RequestedEndTime: &end, RequestedEndFile: &endFile, RequestedEndPos: &endPos,
		SourceDatabaseName: &database, SourceTables: model.StringList{"orders"}, DMLTypes: model.StringList{"update"}}
	req, completion, err := buildRequest(job, &model.PlatformSettings{MySQLRollbackMy2SQLPath: "my2sql", MySQLRollbackGenerationTimeoutSeconds: 30},
		&model.DBConnection{Host: "mysql.internal", Port: 3306, Username: "reader"}, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if req.TimeRange == nil || req.TimeRange.Start != "2026-10-01 08:00:00" || req.TimeRange.End != "2026-10-01 08:05:00" {
		t.Fatalf("unexpected time snapshot: %#v", req.TimeRange)
	}
	if len(req.Databases) != 1 || req.Databases[0] != "app" || len(req.Tables) != 1 || req.Tables[0] != "orders" {
		t.Fatalf("filters were not preserved: %#v", req)
	}
	if completion.ActualStartFile != nil || completion.ActualStartPos != nil || completion.ActualEndFile == nil || *completion.ActualEndFile != endFile || completion.ActualEndPos == nil || *completion.ActualEndPos != endPos {
		t.Fatalf("time mode must retain its hard end snapshot: %#v", completion)
	}
}

func TestWorkerConcurrencyIsGlobalTwoAndOnePerConnection(t *testing.T) {
	w := &Worker{global: make(chan struct{}, 2), active: make(map[uint64]struct{})}
	if !w.acquire(1) {
		t.Fatal("first connection should acquire a slot")
	}
	if w.acquire(1) {
		t.Fatal("same connection must not run concurrently")
	}
	if !w.acquire(2) {
		t.Fatal("second connection should acquire the second global slot")
	}
	if w.acquire(3) {
		t.Fatal("third connection must wait for a global slot")
	}
	w.release(1)
	w.release(2)
}

func TestWorkerCancellationMarksCancelledInsteadOfFailed(t *testing.T) {
	store := &fakeJobStore{status: model.MySQLBinlogExportStatusCancelRequested, cancelled: make(chan struct{})}
	w := NewWorker(store, fakeConnectionStore{}, fakeSettingsStore{}, nil)
	w.run = func(ctx context.Context, _ my2sql.Request) (my2sql.Result, error) {
		<-ctx.Done()
		return my2sql.Result{}, ctx.Err()
	}
	startFile, endFile := "mysql-bin.000001", "mysql-bin.000001"
	startPos, endPos := uint64(4), uint64(900)
	w.execute(context.Background(), &model.MySQLBinlogExportJob{ID: 1, SourceConnectionID: 10, RangeMode: "position",
		RequestedStartFile: &startFile, RequestedStartPos: &startPos, RequestedEndFile: &endFile, RequestedEndPos: &endPos, CreatedAt: time.Now()})
	select {
	case <-store.cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancelled job was not marked cancelled")
	}
	if store.failed {
		t.Fatal("cancelled job must not be marked failed")
	}
}

func TestWorkerDoesNotStartRollbackAfterCancellationBetweenPhases(t *testing.T) {
	store := &fakeJobStore{status: model.MySQLBinlogExportStatusRunning, cancelled: make(chan struct{})}
	w := NewWorker(store, fakeConnectionStore{}, fakeSettingsStore{}, nil)
	runs := 0
	w.run = func(_ context.Context, _ my2sql.Request) (my2sql.Result, error) {
		runs++
		store.setStatus(model.MySQLBinlogExportStatusCancelRequested)
		return my2sql.Result{SQL: "INSERT;"}, nil
	}
	w.execute(context.Background(), positionJob())
	if runs != 1 {
		t.Fatalf("rollback started after cancellation: runs=%d", runs)
	}
	if !store.wasCancelled() || store.failed {
		t.Fatalf("cancelled phase boundary must end as cancelled: %#v", store)
	}
}

func TestWorkerRollbackFailureDoesNotPublishArtifacts(t *testing.T) {
	store := &fakeJobStore{status: model.MySQLBinlogExportStatusRunning, cancelled: make(chan struct{})}
	w := NewWorker(store, fakeConnectionStore{}, fakeSettingsStore{}, nil)
	runs := 0
	w.run = func(_ context.Context, _ my2sql.Request) (my2sql.Result, error) {
		runs++
		if runs == 1 {
			return my2sql.Result{SQL: "INSERT;"}, nil
		}
		return my2sql.Result{}, errors.New("generated rollback sql exceeds size limit")
	}
	w.execute(context.Background(), positionJob())
	if store.completed || store.failureCode != "rollback_output_limit" {
		t.Fatalf("partial result must not be published: completed=%v code=%q", store.completed, store.failureCode)
	}
}

func TestWorkerSuccessPublishesForwardAndRollbackTogether(t *testing.T) {
	store := &fakeJobStore{status: model.MySQLBinlogExportStatusRunning, cancelled: make(chan struct{})}
	w := NewWorker(store, fakeConnectionStore{}, fakeSettingsStore{}, nil)
	w.run = func(_ context.Context, req my2sql.Request) (my2sql.Result, error) {
		return my2sql.Result{SQL: string(req.WorkType) + ";", StatementCount: 1}, nil
	}
	w.execute(context.Background(), positionJob())
	if !store.completed || len(store.artifacts) != 2 || store.artifacts[0].Kind == store.artifacts[1].Kind {
		t.Fatalf("success must atomically publish both artifacts: %#v", store.artifacts)
	}
}

func TestWorkerDispatchRecoversPanicAndMarksJobFailed(t *testing.T) {
	job := *positionJob()
	store := &fakeJobStore{status: model.MySQLBinlogExportStatusRunning, cancelled: make(chan struct{}), failedCh: make(chan struct{}, 1), queued: []model.MySQLBinlogExportJob{job}}
	w := NewWorker(store, fakeConnectionStore{}, fakeSettingsStore{}, nil)
	w.run = func(context.Context, my2sql.Request) (my2sql.Result, error) { panic("runner panic") }
	w.dispatch(context.Background())
	select {
	case <-store.failedCh:
	case <-time.After(time.Second):
		t.Fatal("panicking worker was not marked failed")
	}
	if store.failureCode != "worker_panic" {
		t.Fatalf("failure code = %q", store.failureCode)
	}
}

func TestGenerationErrorCodeKeepsStableFailureCategories(t *testing.T) {
	for _, tc := range []struct{ message, want string }{
		{"my2sql forward timed out", "forward_timeout"},
		{"generated forward sql exceeds size limit", "forward_output_limit"},
		{"temporary disk limit exceeded", "forward_output_limit"},
		{"process exited 1", "forward_generation_failed"},
	} {
		if got := generationErrorCode("forward", errors.New(tc.message)); got != tc.want {
			t.Fatalf("generationErrorCode(%q) = %q, want %q", tc.message, got, tc.want)
		}
	}
	if got := generationErrorCode("forward", my2sql.ErrNoMatchingStatements); got != "no_matching_events" {
		t.Fatalf("no-match code = %q", got)
	}
}

func positionJob() *model.MySQLBinlogExportJob {
	startFile, endFile := "mysql-bin.000001", "mysql-bin.000001"
	startPos, endPos := uint64(4), uint64(900)
	return &model.MySQLBinlogExportJob{ID: 1, SourceConnectionID: 10, RangeMode: "position", RequestedStartFile: &startFile,
		RequestedStartPos: &startPos, RequestedEndFile: &endFile, RequestedEndPos: &endPos, CreatedAt: time.Now()}
}

type fakeJobStore struct {
	mu          sync.Mutex
	status      model.MySQLBinlogExportStatus
	cancelled   chan struct{}
	failed      bool
	failureCode string
	completed   bool
	artifacts   []repository.MySQLBinlogExportArtifactInput
	queued      []model.MySQLBinlogExportJob
	failedCh    chan struct{}
}

func (f *fakeJobStore) ListQueued(context.Context, uint) ([]model.MySQLBinlogExportJob, error) {
	return f.queued, nil
}
func (f *fakeJobStore) GetByID(context.Context, uint64) (*model.MySQLBinlogExportJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &model.MySQLBinlogExportJob{Status: f.status}, nil
}
func (f *fakeJobStore) Claim(context.Context, uint64) (bool, error) { return true, nil }
func (f *fakeJobStore) UpdatePhase(context.Context, uint64, string, string) (bool, error) {
	return true, nil
}
func (f *fakeJobStore) MarkCancelled(context.Context, uint64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.cancelled:
	default:
		close(f.cancelled)
	}
	return true, nil
}
func (f *fakeJobStore) MarkFailed(_ context.Context, _ uint64, code string, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = true
	f.failureCode = code
	if f.failedCh != nil {
		select {
		case f.failedCh <- struct{}{}:
		default:
		}
	}
	return true, nil
}
func (f *fakeJobStore) InterruptActive(context.Context) (int64, error)                { return 0, nil }
func (f *fakeJobStore) PurgeExpired(context.Context, uint) (int64, error)             { return 0, nil }
func (f *fakeJobStore) PurgeMetadata(context.Context, time.Time, uint) (int64, error) { return 0, nil }
func (f *fakeJobStore) Complete(_ context.Context, _ uint64, _ repository.MySQLBinlogExportCompletion, artifacts []repository.MySQLBinlogExportArtifactInput) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = true
	f.artifacts = artifacts
	return true, nil
}
func (f *fakeJobStore) setStatus(status model.MySQLBinlogExportStatus) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}
func (f *fakeJobStore) wasCancelled() bool {
	select {
	case <-f.cancelled:
		return true
	default:
		return false
	}
}

type fakeConnectionStore struct{}

func (fakeConnectionStore) GetByID(context.Context, uint64) (*model.DBConnection, error) {
	return &model.DBConnection{ID: 10, DBType: "mysql", Host: "mysql.internal", Port: 3306}, nil
}
func (fakeConnectionStore) ResolveCredential(conn *model.DBConnection, _ string) (*model.DBConnection, string, error) {
	resolved := *conn
	resolved.Username = "reader"
	return &resolved, "secret", nil
}

type fakeSettingsStore struct{}

func (fakeSettingsStore) Get(context.Context) (*model.PlatformSettings, error) {
	return &model.PlatformSettings{MySQLRollbackMy2SQLPath: "my2sql", MySQLRollbackGenerationTimeoutSeconds: 30}, nil
}
