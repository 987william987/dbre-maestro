package sessionmanagement

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const loopPollInterval = time.Second

type loopJobStore interface {
	ListPending(context.Context, uint) ([]model.SessionLoopJob, error)
	GetByID(context.Context, uint64) (*model.SessionLoopJob, error)
	Claim(context.Context, uint64) (bool, error)
	InterruptRunning(context.Context) (int64, error)
	Finish(context.Context, uint64, string, string, string) (bool, error)
	AddKills(context.Context, uint64, uint) (*model.SessionLoopJob, error)
	RecordError(context.Context, uint64, uint, string, string) (*model.SessionLoopJob, error)
	DecryptPrefix(*model.SessionLoopJob) (string, error)
}

type loopConnectionStore interface {
	GetByID(context.Context, uint64) (*model.DBConnection, error)
	ResolveCredential(*model.DBConnection, string) (*model.DBConnection, string, error)
}

type loopSettingsStore interface {
	Get(context.Context) (*model.PlatformSettings, error)
}

type LoopWorker struct {
	jobs        loopJobStore
	connections loopConnectionStore
	settings    loopSettingsStore
	topology    *Service
	audit       *repository.AuditRepo
	logger      *slog.Logger
	mu          sync.Mutex
	active      map[uint64]struct{}
	iterate     func(context.Context, *model.SessionLoopJob, string) (uint, error)
	waitNext    func(context.Context, uint64, time.Duration) bool
}

func NewLoopWorker(jobs loopJobStore, connections loopConnectionStore, settings loopSettingsStore, topology *Service, audit *repository.AuditRepo, logger *slog.Logger) *LoopWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &LoopWorker{jobs: jobs, connections: connections, settings: settings, topology: topology, audit: audit, logger: logger, active: map[uint64]struct{}{}}
}

func (w *LoopWorker) Start(ctx context.Context) {
	if count, err := w.jobs.InterruptRunning(ctx); err != nil {
		w.logger.Warn("session loop kill: interrupt recovery failed", "err", err)
	} else if count > 0 {
		w.logger.Warn("session loop kill: interrupted unfinished jobs", "count", count)
	}
	ticker := time.NewTicker(loopPollInterval)
	defer ticker.Stop()
	w.dispatch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.dispatch(ctx)
		}
	}
}

func (w *LoopWorker) dispatch(ctx context.Context) {
	jobs, err := w.jobs.ListPending(ctx, 20)
	if err != nil {
		w.logger.Warn("session loop kill: list pending failed", "err", err)
		return
	}
	for i := range jobs {
		job := jobs[i]
		w.mu.Lock()
		_, running := w.active[job.ID]
		if !running {
			w.active[job.ID] = struct{}{}
		}
		w.mu.Unlock()
		if running {
			continue
		}
		claimed, err := w.jobs.Claim(ctx, job.ID)
		if err != nil || !claimed {
			w.release(job.ID)
			if err != nil {
				w.logger.Warn("session loop kill: claim failed", "job_id", job.ID, "err", err)
			}
			continue
		}
		go func(jobID uint64) {
			defer w.release(jobID)
			defer func() {
				if recovered := recover(); recovered != nil {
					w.logger.Error("session loop kill: worker panic", "job_id", jobID)
					_, _ = w.jobs.Finish(context.Background(), jobID, model.SessionLoopStatusFailed, "worker_panic", "loop worker failed unexpectedly")
				}
			}()
			w.execute(ctx, jobID)
		}(job.ID)
	}
}

func (w *LoopWorker) release(id uint64) { w.mu.Lock(); delete(w.active, id); w.mu.Unlock() }

func (w *LoopWorker) execute(ctx context.Context, id uint64) {
	job, err := w.jobs.GetByID(ctx, id)
	if err != nil || job == nil {
		w.fail(id, "job_unavailable")
		return
	}
	prefix, err := w.jobs.DecryptPrefix(job)
	if err != nil {
		w.fail(id, "prefix_unavailable")
		return
	}
	for {
		job, err = w.jobs.GetByID(ctx, id)
		if err != nil || job == nil {
			w.fail(id, "job_unavailable")
			return
		}
		if job.Status != model.SessionLoopStatusRunning {
			return
		}
		if job.ExpiresAt != nil && !time.Now().Before(*job.ExpiresAt) {
			_, _ = w.jobs.Finish(context.Background(), id, model.SessionLoopStatusExpired, "", "")
			return
		}
		if job.KillCount >= job.MaxKills {
			_, _ = w.jobs.Finish(context.Background(), id, model.SessionLoopStatusLimitReached, "", "")
			return
		}
		iterate := w.iterate
		if iterate == nil {
			iterate = w.runIteration
		}
		killed, iterationErr := iterate(ctx, job, prefix)
		if iterationErr != nil {
			updated, recordErr := w.jobs.RecordError(context.Background(), id, killed, "iteration_failed", "loop iteration failed")
			w.logger.Warn("session loop kill: iteration failed", "job_id", id, "err", iterationErr)
			if recordErr != nil {
				w.fail(id, "state_update_failed")
				return
			}
			if updated != nil && updated.ConsecutiveErrors >= 3 {
				_, _ = w.jobs.Finish(context.Background(), id, model.SessionLoopStatusFailed, "consecutive_errors", "three consecutive iterations failed")
				return
			}
		} else {
			updated, updateErr := w.jobs.AddKills(context.Background(), id, killed)
			if updateErr != nil {
				w.fail(id, "state_update_failed")
				return
			}
			if updated != nil && updated.KillCount >= updated.MaxKills {
				_, _ = w.jobs.Finish(context.Background(), id, model.SessionLoopStatusLimitReached, "", "")
				return
			}
		}
		waitNext := w.waitNext
		if waitNext == nil {
			waitNext = w.wait
		}
		if !waitNext(ctx, id, time.Duration(job.IntervalSeconds)*time.Second) {
			return
		}
	}
}

func (w *LoopWorker) runIteration(ctx context.Context, job *model.SessionLoopJob, prefix string) (uint, error) {
	conn, err := w.connections.GetByID(ctx, job.ConnectionID)
	if err != nil || conn == nil {
		return 0, errors.New("connection unavailable")
	}
	if normalizeLoopEngine(conn.DBType) != normalizeLoopEngine(job.Engine) {
		return 0, errors.New("connection engine changed")
	}
	resolved, password, err := w.connections.ResolveCredential(conn, model.DBCredentialRoleOperations)
	if err != nil {
		return 0, errors.New("operations credential unavailable")
	}
	if job.TargetMode == "aws" {
		settings, err := w.settings.Get(ctx)
		if err != nil || settings == nil {
			return 0, errors.New("settings unavailable")
		}
		node, err := w.topology.ResolveOwnedNode(ctx, conn, settings.DBMetadataInventoryRegions, job.Region, job.ClusterID, job.NodeID)
		if err != nil {
			return 0, errors.New("AWS target unavailable")
		}
		resolved.Host, resolved.Port = node.Host, node.Port
	} else {
		if _, err := w.topology.ValidateManualTarget(ctx, job.TargetHost, job.TargetPort); err != nil {
			return 0, errors.New("manual target rejected")
		}
		resolved.Host, resolved.Port = job.TargetHost, job.TargetPort
	}
	result, err := ListSessions(ctx, resolved, password)
	if err != nil {
		return 0, errors.New("session list failed")
	}
	matches := MatchPrefixSessions(result.Items, job.DatabaseName, prefix, float64(job.MinimumAgeSeconds), job.Engine)
	remaining := job.MaxKills - job.KillCount
	var killed uint
	var signalErr bool
	for i := range matches {
		if killed >= remaining {
			break
		}
		if err := SignalSession(ctx, resolved, password, matches[i].ID, "cancel"); err != nil {
			signalErr = true
			w.auditSignal(job, resolved, &matches[i], "failed")
			continue
		}
		killed++
		w.auditSignal(job, resolved, &matches[i], "succeeded")
	}
	if signalErr {
		return killed, errors.New("one or more session signals failed")
	}
	return killed, nil
}

func (w *LoopWorker) wait(ctx context.Context, id uint64, duration time.Duration) bool {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return true
		case <-poll.C:
			job, err := w.jobs.GetByID(ctx, id)
			if err != nil || job == nil || job.Status != model.SessionLoopStatusRunning {
				return false
			}
		}
	}
}

func (w *LoopWorker) fail(id uint64, code string) {
	_, _ = w.jobs.Finish(context.Background(), id, model.SessionLoopStatusFailed, code, "loop worker failed")
}

func (w *LoopWorker) auditSignal(job *model.SessionLoopJob, target *model.DBConnection, session *Session, result string) {
	if w.audit == nil {
		return
	}
	actorID, connectionID := job.RequestedBy, job.ConnectionID
	details := map[string]any{"engine": job.Engine, "target_host": target.Host, "target_port": target.Port, "session_id": session.ID, "db_user": session.User, "database": session.Database, "client": session.Client, "query_hash": session.QueryHash, "query_shape": SanitizeSQLShape(session.Query), "loop_job_id": job.ID, "result": result}
	if err := w.audit.Log(context.Background(), repository.AuditEntry{ActorID: &actorID, ActionType: "db_session_loop_cancel", ResourceType: "db_connection", ResourceID: &connectionID, Details: details}); err != nil {
		w.logger.Warn("session loop kill: audit failed", "job_id", job.ID, "err", err)
	}
}

func normalizeLoopEngine(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
