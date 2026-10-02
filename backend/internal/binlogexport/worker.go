package binlogexport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/my2sql"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	pollInterval     = time.Second
	cancelPoll       = 250 * time.Millisecond
	globalWorkers    = 2
	artifactTTL      = 7 * 24 * time.Hour
	artifactMaxBytes = 25 << 20
)

type jobStore interface {
	ListQueued(context.Context, uint) ([]model.MySQLBinlogExportJob, error)
	GetByID(context.Context, uint64) (*model.MySQLBinlogExportJob, error)
	Claim(context.Context, uint64) (bool, error)
	UpdatePhase(context.Context, uint64, string, string) (bool, error)
	MarkCancelled(context.Context, uint64) (bool, error)
	MarkFailed(context.Context, uint64, string, string) (bool, error)
	InterruptActive(context.Context) (int64, error)
	PurgeExpired(context.Context, uint) (int64, error)
	PurgeMetadata(context.Context, time.Time, uint) (int64, error)
	Complete(context.Context, uint64, repository.MySQLBinlogExportCompletion, []repository.MySQLBinlogExportArtifactInput) (bool, error)
}

type connectionStore interface {
	GetByID(context.Context, uint64) (*model.DBConnection, error)
	ResolveCredential(*model.DBConnection, string) (*model.DBConnection, string, error)
}

type settingsStore interface {
	Get(context.Context) (*model.PlatformSettings, error)
}

type runFunc func(context.Context, my2sql.Request) (my2sql.Result, error)

type Worker struct {
	jobs        jobStore
	connections connectionStore
	settings    settingsStore
	run         runFunc
	logger      *slog.Logger
	global      chan struct{}
	mu          sync.Mutex
	active      map[uint64]struct{}
}

func NewWorker(jobs jobStore, connections connectionStore, settings settingsStore, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{jobs: jobs, connections: connections, settings: settings, run: my2sql.Run,
		logger: logger, global: make(chan struct{}, globalWorkers), active: make(map[uint64]struct{})}
}

func (w *Worker) Start(ctx context.Context) {
	if count, err := w.jobs.InterruptActive(ctx); err != nil {
		w.logger.Warn("binlog export: recover active jobs failed", "err", err)
	} else if count > 0 {
		w.logger.Warn("binlog export: interrupted unfinished jobs", "count", count)
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()
	w.purgeExpired(ctx)
	w.dispatch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.dispatch(ctx)
		case <-cleanupTicker.C:
			w.purgeExpired(ctx)
		}
	}
}

func (w *Worker) dispatch(ctx context.Context) {
	jobs, err := w.jobs.ListQueued(ctx, 20)
	if err != nil {
		w.logger.Warn("binlog export: list queued jobs failed", "err", err)
		return
	}
	for i := range jobs {
		job := jobs[i]
		if !w.acquire(job.SourceConnectionID) {
			continue
		}
		claimed, err := w.jobs.Claim(ctx, job.ID)
		if err != nil || !claimed {
			w.release(job.SourceConnectionID)
			if err != nil {
				w.logger.Warn("binlog export: claim job failed", "job_id", job.ID, "err", err)
			}
			continue
		}
		go func() {
			defer w.release(job.SourceConnectionID)
			defer func() {
				if recovered := recover(); recovered != nil {
					w.logger.Error("binlog export: worker panic", "job_id", job.ID, "panic", recovered)
					_, _ = w.jobs.MarkFailed(context.Background(), job.ID, "worker_panic", "binlog export worker failed unexpectedly")
				}
			}()
			w.execute(ctx, &job)
		}()
	}
}

func (w *Worker) purgeExpired(ctx context.Context) {
	for {
		count, err := w.jobs.PurgeExpired(ctx, 100)
		if err != nil {
			w.logger.Warn("binlog export: purge expired artifacts failed", "err", err)
			return
		}
		if count < 100 {
			break
		}
	}
	if count, err := w.jobs.PurgeMetadata(ctx, time.Now().UTC().Add(-90*24*time.Hour), 100); err != nil {
		w.logger.Warn("binlog export: purge expired metadata failed", "err", err)
	} else if count == 100 {
		w.logger.Info("binlog export: metadata purge batch reached limit", "count", count)
	}
}

func (w *Worker) acquire(connectionID uint64) bool {
	select {
	case w.global <- struct{}{}:
	default:
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.active[connectionID]; exists {
		<-w.global
		return false
	}
	w.active[connectionID] = struct{}{}
	return true
}

func (w *Worker) release(connectionID uint64) {
	w.mu.Lock()
	delete(w.active, connectionID)
	w.mu.Unlock()
	<-w.global
}

func (w *Worker) execute(parent context.Context, job *model.MySQLBinlogExportJob) {
	started := time.Now()
	completion := repository.MySQLBinlogExportCompletion{}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cancelled := make(chan struct{})
	go w.watchCancellation(ctx, job.ID, cancel, cancelled)
	fail := func(code string, err error) {
		if parent.Err() != nil {
			return
		}
		current, getErr := w.jobs.GetByID(context.Background(), job.ID)
		if getErr == nil && current != nil && current.Status == model.MySQLBinlogExportStatusCancelRequested {
			_, _ = w.jobs.MarkCancelled(context.Background(), job.ID)
			return
		}
		select {
		case <-cancelled:
			_, _ = w.jobs.MarkCancelled(context.Background(), job.ID)
		default:
			_, _ = w.jobs.MarkFailed(context.Background(), job.ID, code, err.Error())
		}
		w.logCompletion(job, completion, started, 0, 0, "failed", code)
	}

	settings, err := w.settings.Get(ctx)
	if err != nil || settings == nil {
		if err == nil {
			err = errors.New("platform settings not found")
		}
		fail("settings_unavailable", err)
		return
	}
	conn, err := w.connections.GetByID(ctx, job.SourceConnectionID)
	if err != nil || conn == nil {
		if err == nil {
			err = errors.New("source connection not found")
		}
		fail("connection_unavailable", err)
		return
	}
	if !strings.EqualFold(conn.DBType, "mysql") {
		fail("unsupported_connection", errors.New("source connection is not MySQL"))
		return
	}
	resolved, password, err := w.connections.ResolveCredential(conn, model.DBCredentialRoleRollback)
	if err != nil {
		fail("credential_unavailable", err)
		return
	}
	base, builtCompletion, err := buildRequest(job, settings, resolved, password)
	if err != nil {
		fail("invalid_range", err)
		return
	}
	completion = builtCompletion
	completion.QueueWaitMs = uint64(time.Since(job.CreatedAt).Milliseconds())
	completion.RangeResolveMs = uint64(time.Since(started).Milliseconds())

	_, _ = w.jobs.UpdatePhase(ctx, job.ID, "generating_forward", "")
	phaseStarted := time.Now()
	base.WorkType = my2sql.WorkTypeForward
	forward, err := w.run(ctx, base)
	completion.ForwardGenerationMs = uint64(time.Since(phaseStarted).Milliseconds())
	if err != nil {
		fail(generationErrorCode("forward", err), err)
		return
	}
	if w.cancelRequested(job.ID) {
		_, _ = w.jobs.MarkCancelled(context.Background(), job.ID)
		return
	}
	_, _ = w.jobs.UpdatePhase(ctx, job.ID, "generating_rollback", "")
	phaseStarted = time.Now()
	base.WorkType = my2sql.WorkTypeRollback
	rollback, err := w.run(ctx, base)
	completion.RollbackGenerationMs = uint64(time.Since(phaseStarted).Milliseconds())
	if err != nil {
		fail(generationErrorCode("rollback", err), err)
		return
	}

	completion.Generator = "my2sql"
	completion.GeneratorVersion = firstNonEmpty(forward.Version, rollback.Version)
	completion.TotalDurationMs = uint64(time.Since(started).Milliseconds())
	expiresAt := time.Now().UTC().Add(artifactTTL)
	artifacts := []repository.MySQLBinlogExportArtifactInput{
		{Kind: model.MySQLBinlogExportArtifactForwardSQL, SQL: forward.SQL, StatementCount: uint(forward.StatementCount), ExpiresAt: expiresAt},
		{Kind: model.MySQLBinlogExportArtifactRollbackSQL, SQL: rollback.SQL, StatementCount: uint(rollback.StatementCount), ExpiresAt: expiresAt},
	}
	_, _ = w.jobs.UpdatePhase(ctx, job.ID, "persisting_artifacts", "")
	phaseStarted = time.Now()
	completed, err := w.jobs.Complete(ctx, job.ID, completion, artifacts)
	completion.ArtifactPersistMs = uint64(time.Since(phaseStarted).Milliseconds())
	if err != nil {
		fail("artifact_persist_failed", err)
		return
	}
	if !completed {
		fail("job_state_changed", errors.New("job is no longer running"))
		return
	}
	w.logCompletion(job, completion, started, len(forward.SQL), len(rollback.SQL), "succeeded", "")
}

func (w *Worker) cancelRequested(id uint64) bool {
	job, err := w.jobs.GetByID(context.Background(), id)
	return err == nil && job != nil && job.Status == model.MySQLBinlogExportStatusCancelRequested
}

func generationErrorCode(phase string, err error) string {
	if errors.Is(err, my2sql.ErrNoMatchingStatements) {
		return "no_matching_events"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "timed out") {
		return phase + "_timeout"
	}
	if strings.Contains(message, "size limit") || strings.Contains(message, "disk limit") {
		return phase + "_output_limit"
	}
	return phase + "_generation_failed"
}

func (w *Worker) logCompletion(job *model.MySQLBinlogExportJob, timing repository.MySQLBinlogExportCompletion, started time.Time, forwardBytes, rollbackBytes int, status, code string) {
	w.logger.Log(context.Background(), slog.LevelInfo, "binlog export: job completed",
		"job_id", job.ID, "connection_id", job.SourceConnectionID, "terminal_status", status, "error_code", code,
		"queue_wait_ms", timing.QueueWaitMs, "range_resolve_ms", timing.RangeResolveMs,
		"forward_generation_ms", timing.ForwardGenerationMs, "rollback_generation_ms", timing.RollbackGenerationMs,
		"artifact_persist_ms", timing.ArtifactPersistMs, "total_duration_ms", time.Since(started).Milliseconds(),
		"forward_sql_bytes", forwardBytes, "rollback_sql_bytes", rollbackBytes,
		"filter_database", job.SourceDatabaseName != nil, "filter_table_count", len(job.SourceTables), "dml_type_count", len(job.DMLTypes))
}

func (w *Worker) watchCancellation(ctx context.Context, id uint64, cancel context.CancelFunc, cancelled chan<- struct{}) {
	ticker := time.NewTicker(cancelPoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := w.jobs.GetByID(ctx, id)
			if err == nil && job != nil && job.Status == model.MySQLBinlogExportStatusCancelRequested {
				close(cancelled)
				cancel()
				return
			}
		}
	}
}

func buildRequest(job *model.MySQLBinlogExportJob, settings *model.PlatformSettings, conn *model.DBConnection, password string) (my2sql.Request, repository.MySQLBinlogExportCompletion, error) {
	request := my2sql.Request{BinaryPath: settings.MySQLRollbackMy2SQLPath, Host: conn.Host, Port: conn.Port,
		Username: conn.Username, Password: password, Tables: job.SourceTables, SQLTypes: job.DMLTypes,
		Timeout: time.Duration(settings.MySQLRollbackGenerationTimeoutSeconds) * time.Second, MaxBytes: artifactMaxBytes}
	if job.SourceDatabaseName != nil && strings.TrimSpace(*job.SourceDatabaseName) != "" {
		request.Databases = []string{strings.TrimSpace(*job.SourceDatabaseName)}
	}
	completion := repository.MySQLBinlogExportCompletion{}
	switch job.RangeMode {
	case "position":
		if job.RequestedStartFile == nil || job.RequestedStartPos == nil || job.RequestedEndFile == nil || job.RequestedEndPos == nil {
			return request, completion, errors.New("position range is incomplete")
		}
		request.Range = my2sql.PositionRange{StartFile: *job.RequestedStartFile, StartPos: *job.RequestedStartPos, EndFile: *job.RequestedEndFile, EndPos: *job.RequestedEndPos}
		completion.ActualStartFile, completion.ActualStartPos = job.RequestedStartFile, job.RequestedStartPos
		completion.ActualEndFile, completion.ActualEndPos = job.RequestedEndFile, job.RequestedEndPos
	case "time":
		if job.RequestedStartTime == nil || job.RequestedEndTime == nil {
			return request, completion, errors.New("time range is incomplete")
		}
		location, err := time.LoadLocation(job.Timezone)
		if err != nil {
			return request, completion, fmt.Errorf("invalid timezone: %w", err)
		}
		request.TimeRange = &my2sql.TimeRange{Start: job.RequestedStartTime.In(location).Format("2006-01-02 15:04:05"), End: job.RequestedEndTime.In(location).Format("2006-01-02 15:04:05")}
		if job.RequestedEndFile == nil || job.RequestedEndPos == nil {
			return request, completion, errors.New("time range snapshot is incomplete")
		}
		request.Range.EndFile, request.Range.EndPos = *job.RequestedEndFile, *job.RequestedEndPos
		completion.ActualEndFile, completion.ActualEndPos = job.RequestedEndFile, job.RequestedEndPos
	default:
		return request, completion, fmt.Errorf("unsupported range mode %q", job.RangeMode)
	}
	return request, completion, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
