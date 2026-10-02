package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	appcrypto "github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

type SessionLoopJobRepo struct {
	db     *sqlx.DB
	encKey []byte
}

var ErrSessionLoopTargetActive = errors.New("an active loop job already exists for target")

type SessionLoopJobInput struct {
	RequestedBy, ConnectionID                                     uint64
	Engine, TargetMode, Region, ClusterID, NodeID, Host           string
	Port                                                          uint16
	TargetKey, Database, Prefix, PrefixShape, PrefixHash          string
	MinimumAgeSeconds, IntervalSeconds, DurationSeconds, MaxKills uint
}

func NewSessionLoopJobRepo(db *sqlx.DB, encKey []byte) *SessionLoopJobRepo {
	return &SessionLoopJobRepo{db: db, encKey: encKey}
}

func (r *SessionLoopJobRepo) Create(ctx context.Context, input SessionLoopJobInput) (*model.SessionLoopJob, error) {
	encrypted, err := appcrypto.Encrypt(r.encKey, []byte(input.Prefix))
	if err != nil {
		return nil, fmt.Errorf("encrypt loop prefix: %w", err)
	}
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `INSERT INTO db_session_loop_jobs
		(requested_by, connection_id, engine, target_mode, region, cluster_id, node_id, target_host, target_port, target_key, active_target_key, database_name, prefix_encrypted, prefix_shape, prefix_hash, minimum_age_seconds, interval_seconds, duration_seconds, max_kills, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		input.RequestedBy, input.ConnectionID, input.Engine, input.TargetMode, input.Region, input.ClusterID, input.NodeID, input.Host, input.Port, input.TargetKey, input.TargetKey, input.Database, encrypted, input.PrefixShape, input.PrefixHash, input.MinimumAgeSeconds, input.IntervalSeconds, input.DurationSeconds, input.MaxKills, now, now)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, ErrSessionLoopTargetActive
		}
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, uint64(id))
}

func (r *SessionLoopJobRepo) GetByID(ctx context.Context, id uint64) (*model.SessionLoopJob, error) {
	var job model.SessionLoopJob
	err := r.db.GetContext(ctx, &job, `SELECT * FROM db_session_loop_jobs WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &job, err
}

func (r *SessionLoopJobRepo) ListForConnections(ctx context.Context, connectionIDs []uint64, limit, offset uint) ([]model.SessionLoopJob, error) {
	if len(connectionIDs) == 0 {
		return []model.SessionLoopJob{}, nil
	}
	query, args, err := sqlx.In(`SELECT * FROM db_session_loop_jobs WHERE connection_id IN (?) ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, connectionIDs, limit, offset)
	if err != nil {
		return nil, err
	}
	var jobs []model.SessionLoopJob
	if err := r.db.SelectContext(ctx, &jobs, r.db.Rebind(query), args...); err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []model.SessionLoopJob{}
	}
	return jobs, nil
}

func (r *SessionLoopJobRepo) ListPending(ctx context.Context, limit uint) ([]model.SessionLoopJob, error) {
	var jobs []model.SessionLoopJob
	err := r.db.SelectContext(ctx, &jobs, `SELECT * FROM db_session_loop_jobs WHERE status = 'pending' ORDER BY created_at, id LIMIT ?`, limit)
	return jobs, err
}

func (r *SessionLoopJobRepo) Claim(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE db_session_loop_jobs SET status = 'running', started_at = ?, expires_at = DATE_ADD(?, INTERVAL duration_seconds SECOND), updated_at = ? WHERE id = ? AND status = 'pending'`, now, now, now, id)
	return changed(result, err)
}

func (r *SessionLoopJobRepo) InterruptRunning(ctx context.Context) (int64, error) {
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE db_session_loop_jobs SET status = 'interrupted', prefix_encrypted = NULL, active_target_key = NULL, completed_at = ?, updated_at = ? WHERE status = 'running'`, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *SessionLoopJobRepo) Stop(ctx context.Context, id uint64) (bool, error) {
	return r.finish(ctx, id, model.SessionLoopStatusStopped, "", "")
}

func (r *SessionLoopJobRepo) Finish(ctx context.Context, id uint64, status, code, message string) (bool, error) {
	return r.finish(ctx, id, status, code, message)
}

func (r *SessionLoopJobRepo) finish(ctx context.Context, id uint64, status, code, message string) (bool, error) {
	if !validLoopTerminal(status) {
		return false, fmt.Errorf("invalid loop terminal status %q", status)
	}
	now := timeutil.NowUTC()
	var errorCode, errorMessage any
	if code != "" {
		errorCode = code
	}
	if message != "" {
		errorMessage = message
	}
	result, err := r.db.ExecContext(ctx, `UPDATE db_session_loop_jobs SET status = ?, prefix_encrypted = NULL, active_target_key = NULL, last_error_code = ?, last_error_message = ?, completed_at = ?, updated_at = ? WHERE id = ? AND status IN ('pending', 'running')`, status, errorCode, errorMessage, now, now, id)
	return changed(result, err)
}

func (r *SessionLoopJobRepo) AddKills(ctx context.Context, id uint64, count uint) (*model.SessionLoopJob, error) {
	now := timeutil.NowUTC()
	if _, err := r.db.ExecContext(ctx, `UPDATE db_session_loop_jobs SET kill_count = kill_count + ?, consecutive_errors = 0, last_error_code = NULL, last_error_message = NULL, updated_at = ? WHERE id = ? AND status = 'running'`, count, now, id); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *SessionLoopJobRepo) RecordError(ctx context.Context, id uint64, kills uint, code, message string) (*model.SessionLoopJob, error) {
	now := timeutil.NowUTC()
	if len(message) > 255 {
		message = message[:255]
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE db_session_loop_jobs SET kill_count = kill_count + ?, consecutive_errors = consecutive_errors + 1, last_error_code = ?, last_error_message = ?, updated_at = ? WHERE id = ? AND status = 'running'`, kills, code, message, now, id); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *SessionLoopJobRepo) DecryptPrefix(job *model.SessionLoopJob) (string, error) {
	if job == nil || len(job.PrefixEncrypted) == 0 {
		return "", errors.New("loop prefix is unavailable")
	}
	plain, err := appcrypto.Decrypt(r.encKey, job.PrefixEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt loop prefix: %w", err)
	}
	return string(plain), nil
}

func changed(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func validLoopTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case model.SessionLoopStatusCompleted, model.SessionLoopStatusStopped, model.SessionLoopStatusExpired, model.SessionLoopStatusLimitReached, model.SessionLoopStatusFailed, model.SessionLoopStatusInterrupted:
		return true
	default:
		return false
	}
}
