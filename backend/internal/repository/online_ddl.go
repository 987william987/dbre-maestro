package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/onlineddl"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

const onlineDDLEventCap = 1000
const onlineDDLEventRetention = 30 * 24 * time.Hour

type OnlineDDLRepo struct{ db *sqlx.DB }

type OnlineDDLCreateInput struct {
	TicketID, ExecutionID, ConnectionID, ExecutorID uint64
	Mode, SQLSHA256, PreflightSHA256, ToolVersion   string
	Parameters                                      any
}

func NewOnlineDDLRepo(db *sqlx.DB) *OnlineDDLRepo { return &OnlineDDLRepo{db: db} }

func (r *OnlineDDLRepo) CreateQueued(ctx context.Context, input OnlineDDLCreateInput) (*model.OnlineDDLRun, error) {
	params, err := json.Marshal(input.Parameters)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	if _, err = tx.ExecContext(ctx, `DELETE FROM ticket_online_ddl_runs WHERE ticket_id = ? AND execution_id = ? AND status = 'planned'`, input.TicketID, input.ExecutionID); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO ticket_online_ddl_runs (ticket_id, execution_id, connection_id, executor_id, mode, status, initial_parameters, effective_parameters, sql_sha256, preflight_sha256, tool_version, active_connection_id, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?, ?, 1, ?, ?)`, input.TicketID, input.ExecutionID, input.ConnectionID, input.ExecutorID, input.Mode, params, params, input.SQLSHA256, input.PreflightSHA256, input.ToolVersion, input.ConnectionID, now, now)
	if isDuplicate(err) {
		return nil, onlineddl.ErrConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	changed, err := execChanged(tx.ExecContext(ctx, `UPDATE ticket_executions SET status = 'running', started_at = COALESCE(started_at, ?), error_msg = NULL WHERE id = ? AND ticket_id = ? AND status = 'pending'`, now, input.ExecutionID, input.TicketID))
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, onlineddl.ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, uint64(id))
}

func (r *OnlineDDLRepo) GetByID(ctx context.Context, id uint64) (*model.OnlineDDLRun, error) {
	var run model.OnlineDDLRun
	err := r.db.GetContext(ctx, &run, `SELECT * FROM ticket_online_ddl_runs WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (r *OnlineDDLRepo) GetByExecution(ctx context.Context, ticketID, executionID uint64) (*model.OnlineDDLRun, error) {
	var run model.OnlineDDLRun
	err := r.db.GetContext(ctx, &run, `SELECT * FROM ticket_online_ddl_runs WHERE ticket_id = ? AND execution_id = ?`, ticketID, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (r *OnlineDDLRepo) ListByTicket(ctx context.Context, ticketID uint64) ([]model.OnlineDDLRun, error) {
	runs := []model.OnlineDDLRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT * FROM ticket_online_ddl_runs WHERE ticket_id = ? AND status <> 'planned' ORDER BY execution_id`, ticketID)
	return runs, err
}

func (r *OnlineDDLRepo) ListQueued(ctx context.Context, limit uint) ([]model.OnlineDDLRun, error) {
	if limit == 0 || limit > 100 {
		limit = 100
	}
	runs := []model.OnlineDDLRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT * FROM ticket_online_ddl_runs WHERE status = 'queued' ORDER BY created_at, id LIMIT ?`, limit)
	return runs, err
}

func (r *OnlineDDLRepo) ListActive(ctx context.Context) ([]model.OnlineDDLRun, error) {
	runs := []model.OnlineDDLRun{}
	err := r.db.SelectContext(ctx, &runs, `SELECT * FROM ticket_online_ddl_runs WHERE status IN ('queued','running','paused','cancel_requested') ORDER BY id`)
	return runs, err
}

func (r *OnlineDDLRepo) Claim(ctx context.Context, id, version uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = 'running', phase = 'starting', heartbeat_at = ?, started_at = COALESCE(started_at, ?), version = version + 1, updated_at = ? WHERE id = ? AND status = 'queued' AND version = ?`, now, now, now, id, version))
}

func (r *OnlineDDLRepo) Heartbeat(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET heartbeat_at = ?, updated_at = ? WHERE id = ? AND status IN ('running','paused','cancel_requested')`, now, now, id))
}

func (r *OnlineDDLRepo) FinishRunning(ctx context.Context, id uint64, status, errorCode string) (bool, error) {
	if !onlineDDLTerminal(status) {
		return false, onlineddl.ErrInvalidTransition
	}
	now := timeutil.NowUTC()
	var code any
	if errorCode != "" {
		code = errorCode
	}
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = ?, phase = 'finished', eta_seconds = NULL, eta_display = NULL, active_connection_id = NULL, error_code = ?, version = version + 1, finished_at = ?, updated_at = ? WHERE id = ? AND status IN ('running','paused','cancel_requested')`, status, code, now, now, id))
}

func (r *OnlineDDLRepo) FinishOutcome(ctx context.Context, id uint64, status, errorCode, confidence string, artifacts onlineddl.ArtifactSummary) (bool, error) {
	if !onlineDDLTerminal(status) {
		return false, onlineddl.ErrInvalidTransition
	}
	artifactJSON, err := json.Marshal(artifacts)
	if err != nil {
		return false, err
	}
	now := timeutil.NowUTC()
	var code any
	if errorCode != "" {
		code = errorCode
	}
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = ?, phase = 'finished', eta_seconds = NULL, eta_display = NULL, active_connection_id = NULL, error_code = ?, outcome_confidence = ?, artifact_summary = ?, version = version + 1, finished_at = ?, updated_at = ? WHERE id = ? AND status IN ('running','paused','cancel_requested')`, status, code, confidence, artifactJSON, now, now, id))
}

func (r *OnlineDDLRepo) SaveProgress(ctx context.Context, id uint64, progress onlineddl.ProgressSnapshot, history bool) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	var phase, reason any
	if progress.Phase != "" {
		phase = progress.Phase
	}
	if progress.ThrottleReason != "" {
		reason = progress.ThrottleReason
	}
	result, err := tx.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET phase = COALESCE(?, phase), progress_percent = ?, copied_rows = ?, eta_seconds = NULL, eta_display = ?, replication_lag_ms = ?, threads_running = ?, throttle_reason = ?, heartbeat_at = ?, updated_at = ? WHERE id = ? AND status IN ('running','paused','cancel_requested')`, phase, progress.ProgressPercent, progress.CopiedRows, progress.ETADisplay, progress.ReplicationLagMs, progress.ThreadsRunning, reason, now, now, id)
	changed, err := execChanged(result, err)
	if err != nil || !changed {
		return changed, err
	}
	if history {
		if _, err = tx.ExecContext(ctx, `INSERT INTO ticket_online_ddl_events (run_id, event_type, phase, progress_percent, created_at) VALUES (?, 'progress', ?, ?, ?)`, id, phase, progress.ProgressPercent, now); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM ticket_online_ddl_events WHERE created_at < ? OR (run_id = ? AND id NOT IN (SELECT id FROM (SELECT id FROM ticket_online_ddl_events WHERE run_id = ? ORDER BY created_at DESC, id DESC LIMIT ?) recent))`, now.Add(-onlineDDLEventRetention), id, id, onlineDDLEventCap); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (r *OnlineDDLRepo) SetPaused(ctx context.Context, id, version uint64, paused bool) (bool, error) {
	from, to := onlineddl.StatusRunning, onlineddl.StatusPaused
	if !paused {
		from, to = onlineddl.StatusPaused, onlineddl.StatusRunning
	}
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = ?, pause_requested = ?, version = version + 1, updated_at = ? WHERE id = ? AND status = ? AND version = ?`, to, paused, now, id, from, version))
}

func (r *OnlineDDLRepo) RequestCancel(ctx context.Context, id, version uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = 'cancel_requested', cancel_requested = 1, version = version + 1, updated_at = ? WHERE id = ? AND status IN ('running','paused') AND version = ?`, now, id, version))
}

func (r *OnlineDDLRepo) UpdateEffectiveParameters(ctx context.Context, id, version uint64, actorID *uint64, before, after json.RawMessage) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	result, err := tx.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET effective_parameters = ?, version = version + 1, updated_at = ? WHERE id = ? AND version = ? AND status IN ('running','paused')`, after, now, id, version)
	changed, err := execChanged(result, err)
	if err != nil || !changed {
		return changed, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ticket_online_ddl_events (run_id, actor_id, event_type, before_parameters, after_parameters, result, created_at) VALUES (?, ?, 'runtime_tune', ?, ?, 'succeeded', ?)`, id, actorID, before, after, now); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ticket_online_ddl_events WHERE created_at < ? OR (run_id = ? AND id NOT IN (SELECT id FROM (SELECT id FROM ticket_online_ddl_events WHERE run_id = ? ORDER BY created_at DESC, id DESC LIMIT ?) recent))`, now.Add(-onlineDDLEventRetention), id, id, onlineDDLEventCap); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func onlineDDLTerminal(status string) bool {
	switch status {
	case onlineddl.StatusCompleted, onlineddl.StatusFailed, onlineddl.StatusInterrupted, onlineddl.StatusCompletedAfterCancel, onlineddl.StatusCancelled, onlineddl.StatusCancelledArtifacts, onlineddl.StatusOutcomeUnknown:
		return true
	default:
		return false
	}
}

func (r *OnlineDDLRepo) Transition(ctx context.Context, id, version uint64, from, to string) error {
	if !onlineddl.CanTransition(from, to) {
		return onlineddl.ErrInvalidTransition
	}
	now := timeutil.NowUTC()
	var active any
	if onlineddl.IsActive(to) {
		active = sqlUint64(id)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = ?, active_connection_id = CASE WHEN ? IS NULL THEN NULL ELSE connection_id END, version = version + 1, started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN ? ELSE started_at END, finished_at = CASE WHEN ? IN ('completed','failed','interrupted','completed_after_cancel','cancelled','cancelled_with_artifacts','outcome_unknown') THEN ? ELSE finished_at END, updated_at = ? WHERE id = ? AND status = ? AND version = ?`, to, active, to, now, to, now, now, id, from, version)
	if isDuplicate(err) {
		return onlineddl.ErrConflict
	}
	ok, err := execChanged(result, err)
	if err != nil {
		return err
	}
	if !ok {
		return onlineddl.ErrStaleVersion
	}
	return nil
}

func (r *OnlineDDLRepo) InterruptActive(ctx context.Context) (int64, error) {
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET status = 'interrupted', eta_seconds = NULL, eta_display = NULL, active_connection_id = NULL, error_code = 'server_restarted', version = version + 1, finished_at = ?, updated_at = ? WHERE status IN ('queued','running','paused','cancel_requested')`, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *OnlineDDLRepo) RecordRecovery(ctx context.Context, id uint64, confidence string, artifacts onlineddl.ArtifactSummary) (bool, error) {
	artifactJSON, err := json.Marshal(artifacts)
	if err != nil {
		return false, err
	}
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE ticket_online_ddl_runs SET outcome_confidence = ?, artifact_summary = ?, updated_at = ? WHERE id = ? AND status = 'interrupted'`, confidence, artifactJSON, now, id))
}

func (r *OnlineDDLRepo) AppendEvent(ctx context.Context, event model.OnlineDDLEvent) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO ticket_online_ddl_events (run_id, actor_id, event_type, phase, progress_percent, before_parameters, after_parameters, result, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.RunID, event.ActorID, event.EventType, event.Phase, event.ProgressPercent, onlineDDLNullableJSON(event.BeforeParameters), onlineDDLNullableJSON(event.AfterParameters), event.Result, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ticket_online_ddl_events WHERE created_at < ? OR (run_id = ? AND id NOT IN (SELECT id FROM (SELECT id FROM ticket_online_ddl_events WHERE run_id = ? ORDER BY created_at DESC, id DESC LIMIT ?) recent))`, now.Add(-onlineDDLEventRetention), event.RunID, event.RunID, onlineDDLEventCap); err != nil {
		return err
	}
	return tx.Commit()
}

func isDuplicate(err error) bool {
	var e *mysqlDriver.MySQLError
	return errors.As(err, &e) && e.Number == 1062
}
func sqlUint64(value uint64) any { return value }
func onlineDDLNullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
