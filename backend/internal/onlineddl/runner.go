package onlineddl

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
)

type runnerStore interface {
	InterruptActive(context.Context) (int64, error)
	ListQueued(context.Context, uint) ([]model.OnlineDDLRun, error)
	Claim(context.Context, uint64, uint64) (bool, error)
	Heartbeat(context.Context, uint64) (bool, error)
	FinishRunning(context.Context, uint64, string, string) (bool, error)
}
type runnerOutcomeStore interface {
	FinishOutcome(context.Context, uint64, string, string, string, ArtifactSummary) (bool, error)
}
type runnerRecoveryStore interface {
	ListActive(context.Context) ([]model.OnlineDDLRun, error)
	RecordRecovery(context.Context, uint64, string, ArtifactSummary) (bool, error)
}
type RecoveryInspector func(context.Context, *model.OnlineDDLRun) (OutcomeEvidence, error)

type RunExecutor func(context.Context, *model.OnlineDDLRun) error
type TerminalAudit func(context.Context, *model.OnlineDDLRun, string, string, string, time.Duration) error

type Runner struct {
	store             runnerStore
	execute           RunExecutor
	audit             TerminalAudit
	Recover           RecoveryInspector
	logger            *slog.Logger
	poll, heartbeat   time.Duration
	maxConcurrent     int
	mu                sync.Mutex
	activeRuns        map[uint64]struct{}
	activeConnections map[uint64]struct{}
	wg                sync.WaitGroup
}

func NewRunner(store runnerStore, execute RunExecutor, audit TerminalAudit, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{store: store, execute: execute, audit: audit, logger: logger, poll: time.Second, heartbeat: 5 * time.Second, maxConcurrent: 2, activeRuns: map[uint64]struct{}{}, activeConnections: map[uint64]struct{}{}}
}

func (r *Runner) Start(ctx context.Context) {
	var active []model.OnlineDDLRun
	recoveryStore, canRecover := r.store.(runnerRecoveryStore)
	if canRecover && r.Recover != nil {
		var err error
		active, err = recoveryStore.ListActive(ctx)
		if err != nil {
			r.logger.Warn("online ddl: list restart recovery runs failed", "err", err)
			active = nil
		}
	}
	if count, err := r.store.InterruptActive(ctx); err != nil {
		r.logger.Warn("online ddl: restart recovery failed", "err", err)
	} else if count > 0 {
		r.logger.Warn("online ddl: interrupted unfinished runs", "count", count)
	}
	for i := range active {
		r.recoverInterrupted(context.WithoutCancel(ctx), recoveryStore, &active[i])
	}
	r.dispatch(ctx)
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return
		case <-ticker.C:
			r.dispatch(ctx)
		}
	}
}

func (r *Runner) recoverInterrupted(ctx context.Context, store runnerRecoveryStore, run *model.OnlineDDLRun) {
	evidence, err := r.Recover(ctx, run)
	confidence := "verified_original_unchanged"
	if err != nil || !evidence.VerificationComplete {
		confidence = StatusOutcomeUnknown
	} else if evidence.TargetDDLApplied {
		confidence = "target_applied"
	} else if evidence.Artifacts.HasArtifacts() {
		confidence = "artifacts_present"
	} else if !evidence.OriginalUnchanged {
		confidence = StatusOutcomeUnknown
	}
	if _, saveErr := store.RecordRecovery(ctx, run.ID, confidence, evidence.Artifacts); saveErr != nil {
		r.logger.Warn("online ddl: persist restart recovery failed", "run_id", run.ID, "err", saveErr)
	}
}

func (r *Runner) dispatch(ctx context.Context) {
	available := r.available()
	if available == 0 {
		return
	}
	runs, err := r.store.ListQueued(ctx, 100)
	if err != nil {
		r.logger.Warn("online ddl: list queued failed", "err", err)
		return
	}
	for i := range runs {
		if available == 0 {
			return
		}
		run := runs[i]
		if !r.reserve(run.ID, run.ConnectionID) {
			continue
		}
		claimed, err := r.store.Claim(ctx, run.ID, run.Version)
		if err != nil || !claimed {
			r.release(run.ID, run.ConnectionID)
			if err != nil {
				r.logger.Warn("online ddl: claim failed", "run_id", run.ID, "err", err)
			}
			continue
		}
		available--
		r.wg.Add(1)
		go r.run(ctx, &run)
	}
}

func (r *Runner) run(parent context.Context, run *model.OnlineDDLRun) {
	defer r.wg.Done()
	defer r.release(run.ID, run.ConnectionID)
	started := time.Now()
	runCtx, cancel := context.WithCancel(parent)
	defer cancel()
	heartbeatErr := make(chan error, 1)
	heartbeatDone := make(chan struct{})
	go r.heartbeatLoop(runCtx, run.ID, cancel, heartbeatErr, heartbeatDone)
	execErr, panicked := r.executeSafely(runCtx, run)
	cancel()
	<-heartbeatDone
	var hbErr error
	select {
	case hbErr = <-heartbeatErr:
	default:
	}
	status, code := StatusCompleted, ""
	detail := ""
	var terminal *TerminalError
	if execErr != nil {
		_ = errors.As(execErr, &terminal)
	}
	switch {
	case parent.Err() != nil:
		status, code = StatusInterrupted, "server_shutdown"
	case panicked:
		status, code = StatusFailed, "worker_panic"
	case hbErr != nil:
		status, code = StatusFailed, "heartbeat_failed"
	case execErr != nil:
		if terminal != nil {
			status, code = terminal.Status, terminal.Code
			detail = terminal.Detail
		} else {
			status, code = StatusFailed, runnerErrorCode(execErr)
		}
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	var changed bool
	var err error
	if terminal != nil && terminal.Confidence != "" {
		if store, ok := r.store.(runnerOutcomeStore); ok {
			changed, err = store.FinishOutcome(finishCtx, run.ID, status, code, terminal.Confidence, terminal.Artifacts)
		} else {
			changed, err = r.store.FinishRunning(finishCtx, run.ID, status, code)
		}
	} else {
		changed, err = r.store.FinishRunning(finishCtx, run.ID, status, code)
	}
	if err != nil || !changed {
		r.logger.Warn("online ddl: persist terminal status failed", "run_id", run.ID, "status", status, "err", err)
		return
	}
	duration := time.Since(started)
	r.logger.Info("online ddl: run finished", "run_id", run.ID, "status", status, "error_code", code, "duration_ms", duration.Milliseconds())
	if r.audit != nil {
		if err := r.audit(context.Background(), run, status, code, detail, duration); err != nil {
			r.logger.Warn("online ddl: terminal audit failed", "run_id", run.ID, "status", status, "err", err)
		}
	}
}

func runnerErrorCode(err error) string {
	for _, candidate := range []error{ErrModeDisabled, ErrUnsupportedStatement, ErrPreflightChanged, ErrToolUnavailable, ErrConflict, ErrInvalidParameters, ErrToolProcessFailed} {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	return ErrToolProcessFailed.Error()
}

func (r *Runner) executeSafely(ctx context.Context, run *model.OnlineDDLRun) (err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicked = true
			err = errors.New("online ddl executor panic")
		}
	}()
	if r.execute == nil {
		return errors.New("online ddl executor unavailable"), false
	}
	return r.execute(ctx, run), false
}

func (r *Runner) heartbeatLoop(ctx context.Context, id uint64, cancel context.CancelFunc, failed chan<- error, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := r.store.Heartbeat(ctx, id)
			if err != nil || !ok {
				if err == nil {
					err = errors.New("run is no longer running")
				}
				failed <- err
				cancel()
				return
			}
		}
	}
}

func (r *Runner) available() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxConcurrent - len(r.activeRuns)
}
func (r *Runner) reserve(id, connectionID uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.activeRuns) >= r.maxConcurrent {
		return false
	}
	if _, ok := r.activeRuns[id]; ok {
		return false
	}
	if _, ok := r.activeConnections[connectionID]; ok {
		return false
	}
	r.activeRuns[id] = struct{}{}
	r.activeConnections[connectionID] = struct{}{}
	return true
}
func (r *Runner) release(id, connectionID uint64) {
	r.mu.Lock()
	delete(r.activeRuns, id)
	delete(r.activeConnections, connectionID)
	r.mu.Unlock()
}
