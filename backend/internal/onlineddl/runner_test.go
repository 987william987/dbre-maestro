package onlineddl

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

type runnerStoreStub struct {
	mu                 sync.Mutex
	runs               []model.OnlineDDLRun
	claimed            map[uint64]bool
	finished           map[uint64]string
	codes              map[uint64]string
	confidence         map[uint64]string
	artifacts          map[uint64]ArtifactSummary
	heartbeatOK        bool
	interrupts         int
	recoveryConfidence map[uint64]string
}

func newRunnerStore(runs ...model.OnlineDDLRun) *runnerStoreStub {
	return &runnerStoreStub{runs: runs, claimed: map[uint64]bool{}, finished: map[uint64]string{}, codes: map[uint64]string{}, confidence: map[uint64]string{}, artifacts: map[uint64]ArtifactSummary{}, recoveryConfidence: map[uint64]string{}, heartbeatOK: true}
}
func (s *runnerStoreStub) InterruptActive(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupts++
	return 0, nil
}
func (s *runnerStoreStub) ListQueued(context.Context, uint) ([]model.OnlineDDLRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.OnlineDDLRun(nil), s.runs...), nil
}
func (s *runnerStoreStub) ListActive(context.Context) ([]model.OnlineDDLRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.OnlineDDLRun(nil), s.runs...), nil
}
func (s *runnerStoreStub) RecordRecovery(_ context.Context, id uint64, confidence string, artifacts ArtifactSummary) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveryConfidence[id], s.artifacts[id] = confidence, artifacts
	return true, nil
}
func (s *runnerStoreStub) Claim(_ context.Context, id, _ uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed[id] {
		return false, nil
	}
	s.claimed[id] = true
	return true, nil
}
func (s *runnerStoreStub) Heartbeat(context.Context, uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeatOK, nil
}
func (s *runnerStoreStub) FinishRunning(_ context.Context, id uint64, status, code string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished[id], s.codes[id] = status, code
	return true, nil
}
func (s *runnerStoreStub) FinishOutcome(_ context.Context, id uint64, status, code, confidence string, artifacts ArtifactSummary) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished[id], s.codes[id], s.confidence[id], s.artifacts[id] = status, code, confidence, artifacts
	return true, nil
}

func queuedRun(id, connection uint64) model.OnlineDDLRun {
	return model.OnlineDDLRun{ID: id, ConnectionID: connection, Mode: ModeGhost, Status: StatusQueued, Version: 2}
}

func waitRunner(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for runner state")
}
func finishedAs(store *runnerStoreStub, id uint64, status string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.finished[id] == status
}

func TestRunnerBoundsConcurrencyAndSerializesConnection(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10), queuedRun(2, 10), queuedRun(3, 20), queuedRun(4, 30))
	started, release := make(chan uint64, 4), make(chan struct{})
	runner := NewRunner(store, func(_ context.Context, run *model.OnlineDDLRun) error { started <- run.ID; <-release; return nil }, nil, nil)
	runner.maxConcurrent = 2
	runner.dispatch(context.Background())
	first, second := <-started, <-started
	if (first == 2 || second == 2) || first == second {
		t.Fatalf("same connection dispatched concurrently: %d, %d", first, second)
	}
	select {
	case id := <-started:
		t.Fatalf("concurrency cap exceeded by run %d", id)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	waitRunner(t, func() bool {
		return finishedAs(store, first, StatusCompleted) && finishedAs(store, second, StatusCompleted)
	})
	runner.dispatch(context.Background())
	third, fourth := <-started, <-started
	if third != 2 && fourth != 2 {
		t.Fatalf("same-connection run was not dispatched after release: %d, %d", third, fourth)
	}
}

func TestRunnerClaimPreventsTwoExecutorsRunningSameRun(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10))
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	execute := func(context.Context, *model.OnlineDDLRun) error { started <- struct{}{}; <-release; return nil }
	first := NewRunner(store, execute, nil, nil)
	second := NewRunner(store, execute, nil, nil)
	var dispatch sync.WaitGroup
	dispatch.Add(2)
	go func() { defer dispatch.Done(); first.dispatch(context.Background()) }()
	go func() { defer dispatch.Done(); second.dispatch(context.Background()) }()
	dispatch.Wait()
	<-started
	select {
	case <-started:
		t.Fatal("same run reached two executors")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
}

func TestRunnerExecutesAlreadyQueuedRunAfterModeIsDisabledAndPreventsDoubleDispatch(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10))
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	runner := NewRunner(store, func(context.Context, *model.OnlineDDLRun) error { started <- struct{}{}; <-block; return nil }, nil, nil)
	runner.dispatch(context.Background())
	<-started
	runner.dispatch(context.Background())
	store.mu.Lock()
	claims := len(store.claimed)
	store.mu.Unlock()
	if claims != 1 {
		t.Fatalf("claims=%d", claims)
	}
	close(block)
}

func TestRunnerPersistsPanicHeartbeatShutdownAndAuditFailure(t *testing.T) {
	tests := []struct {
		name, wantStatus, wantCode string
		configure                  func(*Runner, *runnerStoreStub, *context.CancelFunc)
	}{
		{name: "panic", wantStatus: StatusFailed, wantCode: "worker_panic", configure: func(r *Runner, _ *runnerStoreStub, _ *context.CancelFunc) {
			r.execute = func(context.Context, *model.OnlineDDLRun) error { panic("boom") }
		}},
		{name: "lost heartbeat", wantStatus: StatusFailed, wantCode: "heartbeat_failed", configure: func(r *Runner, s *runnerStoreStub, _ *context.CancelFunc) {
			s.heartbeatOK = false
			r.heartbeat = 5 * time.Millisecond
			r.execute = func(ctx context.Context, _ *model.OnlineDDLRun) error { <-ctx.Done(); return ctx.Err() }
		}},
		{name: "shutdown", wantStatus: StatusInterrupted, wantCode: "server_shutdown", configure: func(r *Runner, _ *runnerStoreStub, cancel *context.CancelFunc) {
			r.execute = func(ctx context.Context, _ *model.OnlineDDLRun) error { (*cancel)(); <-ctx.Done(); return ctx.Err() }
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newRunnerStore(queuedRun(1, 10))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := NewRunner(store, nil, func(context.Context, *model.OnlineDDLRun, string, string, string, time.Duration) error {
				return errors.New("audit unavailable")
			}, nil)
			tc.configure(runner, store, &cancel)
			runner.dispatch(ctx)
			waitRunner(t, func() bool { return finishedAs(store, 1, tc.wantStatus) })
			store.mu.Lock()
			code := store.codes[1]
			store.mu.Unlock()
			if code != tc.wantCode {
				t.Fatalf("code=%q", code)
			}
		})
	}
}

func TestRunnerInterruptsActiveRunsBeforeDispatch(t *testing.T) {
	store := newRunnerStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	NewRunner(store, nil, nil, nil).Start(ctx)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.interrupts != 1 {
		t.Fatalf("interrupts=%d", store.interrupts)
	}
}

func TestRunnerPersistsShutdownArtifactsFromVerifiedTerminal(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10))
	ctx, cancel := context.WithCancel(context.Background())
	artifacts := ArtifactSummary{Items: []Artifact{{Kind: "ghost_table", Exists: true}}}
	runner := NewRunner(store, func(context.Context, *model.OnlineDDLRun) error {
		cancel()
		return VerifiedTerminalOutcome(StatusInterrupted, "server_shutdown", artifacts)
	}, nil, nil)
	runner.dispatch(ctx)
	waitRunner(t, func() bool { return finishedAs(store, 1, StatusInterrupted) })
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.artifacts[1].HasArtifacts() {
		t.Fatalf("artifacts=%+v", store.artifacts[1])
	}
}

func TestRunnerPreservesStableResolverErrors(t *testing.T) {
	if got := runnerErrorCode(ErrPreflightChanged); got != ErrPreflightChanged.Error() {
		t.Fatalf("code=%q", got)
	}
	if got := runnerErrorCode(errors.New("secret internal failure")); got != ErrToolProcessFailed.Error() {
		t.Fatalf("code=%q", got)
	}
}

func TestRunnerDiscoversRestartArtifactsWithoutMutation(t *testing.T) {
	store := newRunnerStore(queuedRun(7, 10))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := NewRunner(store, nil, nil, nil)
	runner.Recover = func(context.Context, *model.OnlineDDLRun) (OutcomeEvidence, error) {
		return OutcomeEvidence{VerificationComplete: true, OriginalUnchanged: true, Artifacts: ArtifactSummary{Items: []Artifact{{Kind: "ghost_table", Exists: true}}}}, nil
	}
	runner.Start(ctx)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.recoveryConfidence[7] != "artifacts_present" || !store.artifacts[7].HasArtifacts() {
		t.Fatalf("confidence=%q artifacts=%+v", store.recoveryConfidence[7], store.artifacts[7])
	}
}

func TestRunnerPersistsVerifiedCancelOutcome(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10))
	artifacts := ArtifactSummary{Items: []Artifact{{Kind: "ghost_table", Name: "_orders_gho", Exists: true}}}
	runner := NewRunner(store, func(context.Context, *model.OnlineDDLRun) error {
		return VerifiedTerminalOutcome(StatusCancelledArtifacts, ErrCancelledArtifacts.Error(), artifacts)
	}, nil, nil)
	runner.dispatch(context.Background())
	waitRunner(t, func() bool { return finishedAs(store, 1, StatusCancelledArtifacts) })
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.confidence[1] != "verified" || !store.artifacts[1].HasArtifacts() {
		t.Fatalf("confidence=%q artifacts=%+v", store.confidence[1], store.artifacts[1])
	}
}

func TestRunnerPassesTerminalDetailToAudit(t *testing.T) {
	store := newRunnerStore(queuedRun(1, 10))
	const detail = "Error 1419: SUPER privilege is required"
	received := make(chan string, 1)
	runner := NewRunner(store, func(context.Context, *model.OnlineDDLRun) error {
		return VerifiedTerminalOutcomeWithDetail(StatusOutcomeUnknown, ErrOutcomeUnknown.Error(), detail, ArtifactSummary{})
	}, func(_ context.Context, _ *model.OnlineDDLRun, _, _, got string, _ time.Duration) error {
		received <- got
		return nil
	}, nil)
	runner.dispatch(context.Background())
	waitRunner(t, func() bool { return finishedAs(store, 1, StatusOutcomeUnknown) })
	select {
	case got := <-received:
		if got != detail {
			t.Fatalf("detail=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal audit did not receive failure detail")
	}
}
