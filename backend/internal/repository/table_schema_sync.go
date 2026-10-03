package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var ErrTableSchemaSyncTargetActive = errors.New("an active table schema sync job already exists for target")

type TableSchemaSyncRepo struct{ db *sqlx.DB }
type TableSchemaSyncItemInput struct {
	TableName       string
	DependencyOrder uint
	SourceDDLSHA256 string
}
type TableSchemaSyncCreateInput struct {
	RequestedBy, SourceConnectionID, TargetConnectionID uint64
	SourceDatabase, TargetDatabase                      string
	Transformation                                      any
	RetryOfJobID                                        *uint64
	Items                                               []TableSchemaSyncItemInput
}

func NewTableSchemaSyncRepo(db *sqlx.DB) *TableSchemaSyncRepo { return &TableSchemaSyncRepo{db: db} }

func (r *TableSchemaSyncRepo) Create(ctx context.Context, input TableSchemaSyncCreateInput) (*model.TableSchemaSyncJob, error) {
	if len(input.Items) == 0 {
		return nil, errors.New("table schema sync items are required")
	}
	config, err := json.Marshal(input.Transformation)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	targetKey := fmt.Sprintf("%d|%s", input.TargetConnectionID, input.TargetDatabase)
	result, err := tx.ExecContext(ctx, `INSERT INTO table_schema_sync_jobs (requested_by, source_connection_id, source_database, target_connection_id, target_database, transformation_config, status, active_target_key, retry_of_job_id, table_count, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?)`, input.RequestedBy, input.SourceConnectionID, input.SourceDatabase, input.TargetConnectionID, input.TargetDatabase, config, targetKey, input.RetryOfJobID, len(input.Items), now, now)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, ErrTableSchemaSyncTargetActive
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	for _, item := range input.Items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO table_schema_sync_job_items (job_id, table_name, dependency_order, source_ddl_sha256, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'pending', ?, ?)`, id, item.TableName, item.DependencyOrder, item.SourceDDLSHA256, now, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, uint64(id))
}

func (r *TableSchemaSyncRepo) GetByID(ctx context.Context, id uint64) (*model.TableSchemaSyncJob, error) {
	var job model.TableSchemaSyncJob
	err := r.db.GetContext(ctx, &job, `SELECT * FROM table_schema_sync_jobs WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &job, err
}
func (r *TableSchemaSyncRepo) List(ctx context.Context, limit, offset uint) ([]model.TableSchemaSyncJob, error) {
	jobs := []model.TableSchemaSyncJob{}
	err := r.db.SelectContext(ctx, &jobs, `SELECT * FROM table_schema_sync_jobs ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
	return jobs, err
}
func (r *TableSchemaSyncRepo) ListScoped(ctx context.Context, connectionIDs []uint64, limit, offset uint) ([]model.TableSchemaSyncJob, uint64, error) {
	if len(connectionIDs) == 0 {
		return []model.TableSchemaSyncJob{}, 0, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(connectionIDs)), ",")
	where := " WHERE source_connection_id IN (" + marks + ") AND target_connection_id IN (" + marks + ")"
	args := make([]any, 0, len(connectionIDs)*2+2)
	for _, id := range connectionIDs {
		args = append(args, id)
	}
	for _, id := range connectionIDs {
		args = append(args, id)
	}
	var total uint64
	if err := r.db.GetContext(ctx, &total, "SELECT COUNT(*) FROM table_schema_sync_jobs"+where, args...); err != nil {
		return nil, 0, err
	}
	jobs := []model.TableSchemaSyncJob{}
	listArgs := append(append([]any(nil), args...), limit, offset)
	err := r.db.SelectContext(ctx, &jobs, "SELECT * FROM table_schema_sync_jobs"+where+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?", listArgs...)
	return jobs, total, err
}
func (r *TableSchemaSyncRepo) ListItems(ctx context.Context, id uint64) ([]model.TableSchemaSyncJobItem, error) {
	items := []model.TableSchemaSyncJobItem{}
	err := r.db.SelectContext(ctx, &items, `SELECT * FROM table_schema_sync_job_items WHERE job_id = ? ORDER BY dependency_order`, id)
	return items, err
}
func (r *TableSchemaSyncRepo) ListQueued(ctx context.Context, limit uint) ([]model.TableSchemaSyncJob, error) {
	jobs := []model.TableSchemaSyncJob{}
	err := r.db.SelectContext(ctx, &jobs, `SELECT * FROM table_schema_sync_jobs WHERE status = 'queued' ORDER BY created_at, id LIMIT ?`, limit)
	return jobs, err
}
func (r *TableSchemaSyncRepo) Claim(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = 'running', started_at = ?, updated_at = ? WHERE id = ? AND status = 'queued'`, now, now, id))
}

func (r *TableSchemaSyncRepo) RequestCancel(ctx context.Context, id, actorID uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE 'cancel_requested' END, cancel_requested_by = ?, active_target_key = CASE WHEN status = 'queued' THEN NULL ELSE active_target_key END, finished_at = CASE WHEN status = 'queued' THEN ? ELSE finished_at END, updated_at = ? WHERE id = ? AND status IN ('queued','running')`, actorID, now, now, id))
}
func (r *TableSchemaSyncRepo) MarkItemStarted(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE table_schema_sync_job_items SET started_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, now, now, id))
}
func (r *TableSchemaSyncRepo) MarkItemCreated(ctx context.Context, id uint64, hash string, duration time.Duration) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE table_schema_sync_job_items SET status = 'created', target_ddl_sha256 = ?, duration_ms = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, hash, duration.Milliseconds(), now, now, id))
}

func (r *TableSchemaSyncRepo) Fail(ctx context.Context, jobID, itemID uint64, code string, duration time.Duration) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	code = trimTableSchemaError(code)
	if _, err := tx.ExecContext(ctx, `UPDATE table_schema_sync_job_items SET status = 'failed', error_code = ?, duration_ms = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'`, code, duration.Milliseconds(), now, now, itemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE table_schema_sync_job_items SET status = 'not_started', finished_at = ?, updated_at = ? WHERE job_id = ? AND status = 'pending'`, now, now, jobID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = 'failed', active_target_key = NULL, failed_count = 1, not_started_count = (SELECT COUNT(*) FROM table_schema_sync_job_items WHERE job_id = ? AND status = 'not_started'), created_count = (SELECT COUNT(*) FROM table_schema_sync_job_items WHERE job_id = ? AND status = 'created'), error_code = ?, finished_at = ?, updated_at = ? WHERE id = ? AND status IN ('running','cancel_requested')`, jobID, jobID, code, now, now, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *TableSchemaSyncRepo) Complete(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = 'completed', active_target_key = NULL, created_count = table_count, finished_at = ?, updated_at = ? WHERE id = ? AND status = 'running'`, now, now, id))
}
func (r *TableSchemaSyncRepo) MarkCancelled(ctx context.Context, id uint64) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	if _, err := tx.ExecContext(ctx, `UPDATE table_schema_sync_job_items SET status = 'not_started', finished_at = ?, updated_at = ? WHERE job_id = ? AND status = 'pending'`, now, now, id); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = 'cancelled', active_target_key = NULL, not_started_count = (SELECT COUNT(*) FROM table_schema_sync_job_items WHERE job_id = ? AND status = 'not_started'), created_count = (SELECT COUNT(*) FROM table_schema_sync_job_items WHERE job_id = ? AND status = 'created'), finished_at = ?, updated_at = ? WHERE id = ? AND status = 'cancel_requested'`, id, id, now, now, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return false, err
	}
	return true, tx.Commit()
}
func (r *TableSchemaSyncRepo) InterruptRunning(ctx context.Context) (int64, error) {
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE table_schema_sync_jobs SET status = 'interrupted', active_target_key = NULL, error_code = 'server_restarted', finished_at = ?, updated_at = ? WHERE status IN ('running','cancel_requested')`, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func trimTableSchemaError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return value[:64]
	}
	return value
}
