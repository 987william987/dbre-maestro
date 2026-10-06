package tableschema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type syncJobStore interface {
	ListQueued(context.Context, uint) ([]model.TableSchemaSyncJob, error)
	GetByID(context.Context, uint64) (*model.TableSchemaSyncJob, error)
	ListItems(context.Context, uint64) ([]model.TableSchemaSyncJobItem, error)
	Claim(context.Context, uint64) (bool, error)
	InterruptRunning(context.Context) (int64, error)
	MarkItemStarted(context.Context, uint64) (bool, error)
	MarkItemCreated(context.Context, uint64, string, time.Duration) (bool, error)
	Fail(context.Context, uint64, uint64, string, time.Duration) error
	Complete(context.Context, uint64) (bool, error)
	MarkCancelled(context.Context, uint64) (bool, error)
}

type syncConnectionStore interface {
	GetByID(context.Context, uint64) (*model.DBConnection, error)
	CredentialResolver
}

type SyncWorker struct {
	jobs        syncJobStore
	connections syncConnectionStore
	logger      *slog.Logger
	poll        time.Duration
	run         func(context.Context, *model.TableSchemaSyncJob) error
	audit       func(context.Context, repository.AuditEntry) error
}

func NewSyncWorker(jobs syncJobStore, connections syncConnectionStore, audit *repository.AuditRepo, logger *slog.Logger) *SyncWorker {
	if logger == nil {
		logger = slog.Default()
	}
	w := &SyncWorker{jobs: jobs, connections: connections, logger: logger, poll: time.Second}
	if audit != nil {
		w.audit = audit.Log
	}
	w.run = w.runJob
	return w
}

func (w *SyncWorker) Start(ctx context.Context) {
	if count, err := w.jobs.InterruptRunning(ctx); err != nil {
		w.logger.Warn("table schema sync: interrupt recovery failed", "err", err)
	} else if count > 0 {
		w.logger.Warn("table schema sync: interrupted unfinished jobs", "count", count)
	}
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		w.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *SyncWorker) dispatch(ctx context.Context) {
	jobs, err := w.jobs.ListQueued(ctx, 1)
	if err != nil || len(jobs) == 0 {
		if err != nil {
			w.logger.Warn("table schema sync: list queued failed", "err", err)
		}
		return
	}
	claimed, err := w.jobs.Claim(ctx, jobs[0].ID)
	if err != nil || !claimed {
		return
	}
	started := time.Now()
	runErr := w.run(ctx, &jobs[0])
	w.recordTerminal(ctx, &jobs[0], time.Since(started), runErr)
}

func (w *SyncWorker) recordTerminal(ctx context.Context, claimed *model.TableSchemaSyncJob, duration time.Duration, runErr error) {
	job, err := w.jobs.GetByID(context.WithoutCancel(ctx), claimed.ID)
	if err != nil || job == nil {
		w.logger.Warn("table schema sync: terminal status unavailable", "job_id", claimed.ID, "duration_ms", duration.Milliseconds(), "err", err)
		return
	}
	attrs := []any{"job_id", job.ID, "status", job.Status, "duration_ms", duration.Milliseconds(), "created_count", job.CreatedCount, "failed_count", job.FailedCount, "not_started_count", job.NotStartedCount}
	if runErr != nil && ctx.Err() == nil {
		attrs = append(attrs, "err", runErr)
		w.logger.Warn("table schema sync: job finished", attrs...)
	} else {
		w.logger.Info("table schema sync: job finished", attrs...)
	}
	if w.audit == nil || !isSyncTerminal(job.Status) {
		return
	}
	actorID, resourceID := job.RequestedBy, job.ID
	details := map[string]any{"status": job.Status, "source_connection_id": job.SourceConnectionID, "source_database": job.SourceDatabase, "target_connection_id": job.TargetConnectionID, "target_database": job.TargetDatabase, "table_count": job.TableCount, "created_count": job.CreatedCount, "failed_count": job.FailedCount, "not_started_count": job.NotStartedCount, "duration_ms": duration.Milliseconds()}
	if job.ErrorCode != nil {
		details["error_code"] = *job.ErrorCode
	}
	if err := w.audit(context.Background(), repository.AuditEntry{ActorID: &actorID, ActionType: "table_schema_sync_job_finished", ResourceType: "table_schema_sync_job", ResourceID: &resourceID, Details: details}); err != nil {
		w.logger.Warn("table schema sync: terminal audit failed", "job_id", job.ID, "status", job.Status, "err", err)
	}
}

func isSyncTerminal(status string) bool {
	switch status {
	case model.TableSchemaSyncCompleted, model.TableSchemaSyncFailed, model.TableSchemaSyncCancelled, model.TableSchemaSyncInterrupted:
		return true
	default:
		return false
	}
}

func (w *SyncWorker) runJob(parent context.Context, job *model.TableSchemaSyncJob) error {
	items, err := w.jobs.ListItems(parent, job.ID)
	if err != nil || len(items) == 0 {
		return fmt.Errorf("load job items: %w", err)
	}
	sourceConn, err := w.connections.GetByID(parent, job.SourceConnectionID)
	if err != nil || sourceConn == nil {
		return w.fail(job.ID, items[0].ID, "source_connection_unavailable", 0)
	}
	targetConn, err := w.connections.GetByID(parent, job.TargetConnectionID)
	if err != nil || targetConn == nil {
		return w.fail(job.ID, items[0].ID, "target_connection_unavailable", 0)
	}
	source, err := OpenReadonly(parent, w.connections, sourceConn)
	if err != nil {
		return w.fail(job.ID, items[0].ID, "source_connection_unavailable", 0)
	}
	defer source.Close()
	target, err := OpenReadwrite(parent, w.connections, targetConn)
	if err != nil {
		return w.fail(job.ID, items[0].ID, "target_connection_unavailable", 0)
	}
	defer target.Close()
	capabilities, err := LoadCapabilities(parent, target)
	if err != nil {
		return w.fail(job.ID, items[0].ID, "target_capability_changed", 0)
	}
	var config TransformationConfig
	if err := json.Unmarshal(job.TransformationConfig, &config); err != nil {
		return w.fail(job.ID, items[0].ID, "invalid_transformation", 0)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	watcherDone := make(chan struct{})
	go w.watchCancel(ctx, job.ID, cancel, watcherDone)
	defer close(watcherDone)
	for i := range items {
		if ctx.Err() != nil {
			return w.finishCancellation(parent, job.ID)
		}
		started := time.Now()
		snapshot, err := LoadSnapshot(ctx, source, job.SourceDatabase, []string{items[i].TableName})
		if err != nil || snapshot == nil || len(snapshot.Tables) != 1 || hashDDL(snapshot.Tables[0].CreateSQL) != items[i].SourceDDLSHA256 {
			return w.fail(job.ID, items[i].ID, "source_ddl_changed", time.Since(started))
		}
		parsed, err := ParseCreateTable(snapshot.Tables[0].CreateSQL)
		if err != nil {
			return w.fail(job.ID, items[i].ID, "create_table_failed", time.Since(started))
		}
		transformed, err := TransformCreateTable(parsed, job.TargetDatabase, config, capabilities)
		if err != nil {
			return w.fail(job.ID, items[i].ID, "target_capability_changed", time.Since(started))
		}
		if ok, err := w.jobs.MarkItemStarted(context.Background(), items[i].ID); err != nil || !ok {
			return fmt.Errorf("mark item started: %w", err)
		}
		_, execErr := target.ExecContext(ctx, transformed.SQL)
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 5*time.Second)
		targetDDL, readErr := readCreateTable(verifyCtx, target, job.TargetDatabase, items[i].TableName)
		verifyCancel()
		if readErr == nil && (execErr == nil || ctx.Err() != nil) {
			if ok, err := w.jobs.MarkItemCreated(context.Background(), items[i].ID, hashDDL(targetDDL), time.Since(started)); err != nil || !ok {
				return fmt.Errorf("mark item created: %w", err)
			}
			if ctx.Err() != nil {
				return w.finishCancellation(parent, job.ID)
			}
			continue
		}
		if ctx.Err() != nil {
			return w.finishCancellation(parent, job.ID)
		}
		if execErr == nil {
			execErr = readErr
		}
		return w.fail(job.ID, items[i].ID, "create_table_failed", time.Since(started))
	}
	_, err = w.jobs.Complete(context.Background(), job.ID)
	return err
}

func (w *SyncWorker) watchCancel(ctx context.Context, id uint64, cancel context.CancelFunc, done <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			job, err := w.jobs.GetByID(context.Background(), id)
			if err == nil && job != nil && job.Status == model.TableSchemaSyncCancelRequested {
				cancel()
				return
			}
		}
	}
}
func (w *SyncWorker) finishCancellation(parent context.Context, id uint64) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	_, err := w.jobs.MarkCancelled(context.Background(), id)
	return err
}
func (w *SyncWorker) fail(jobID, itemID uint64, code string, duration time.Duration) error {
	if err := w.jobs.Fail(context.Background(), jobID, itemID, code, duration); err != nil {
		return err
	}
	return errors.New(code)
}
func hashDDL(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func readCreateTable(ctx context.Context, db Queryer, database, table string) (string, error) {
	var name, ddl string
	err := db.QueryRowContext(ctx, fmt.Sprintf("SHOW CREATE TABLE %s.%s", QuoteIdentifier(database), QuoteIdentifier(table))).Scan(&name, &ddl)
	if err == nil && !strings.EqualFold(name, table) {
		err = errors.New("unexpected target table")
	}
	return ddl, err
}
