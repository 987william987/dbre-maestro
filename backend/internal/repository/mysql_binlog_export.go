package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

const maxBinlogArtifactPlaintextBytes = 25 << 20

type MySQLBinlogExportRepo struct {
	db     *sqlx.DB
	encKey []byte
}

type MySQLBinlogExportArtifactInput struct {
	Kind           model.MySQLBinlogExportArtifactKind
	SQL            string
	StatementCount uint
	ExpiresAt      time.Time
}

type MySQLBinlogExportCompletion struct {
	Generator            string
	GeneratorVersion     string
	ActualStartFile      *string
	ActualStartPos       *uint64
	ActualEndFile        *string
	ActualEndPos         *uint64
	BinlogFileCount      *uint
	QueueWaitMs          uint64
	RangeResolveMs       uint64
	ForwardGenerationMs  uint64
	RollbackGenerationMs uint64
	ArtifactPersistMs    uint64
	TotalDurationMs      uint64
}

func NewMySQLBinlogExportRepo(db *sqlx.DB, encKey []byte) *MySQLBinlogExportRepo {
	return &MySQLBinlogExportRepo{db: db, encKey: encKey}
}

func (r *MySQLBinlogExportRepo) Create(ctx context.Context, job *model.MySQLBinlogExportJob) (uint64, error) {
	if job == nil {
		return 0, errors.New("binlog export job is required")
	}
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `INSERT INTO mysql_binlog_export_jobs
		(requested_by, source_connection_id, range_mode, timezone, requested_start_time, requested_end_time,
		 requested_start_file, requested_start_pos, requested_end_file, requested_end_pos, source_database_name,
		 source_tables, dml_types, acknowledged_unfiltered, status, phase, generator, retry_of_job_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', 'queued', ?, ?, ?, ?)`,
		job.RequestedBy, job.SourceConnectionID, job.RangeMode, job.Timezone, job.RequestedStartTime, job.RequestedEndTime,
		job.RequestedStartFile, job.RequestedStartPos, job.RequestedEndFile, job.RequestedEndPos, job.SourceDatabaseName,
		job.SourceTables, job.DMLTypes, job.AcknowledgedUnfiltered, job.Generator, job.RetryOfJobID, now, now)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func (r *MySQLBinlogExportRepo) GetByID(ctx context.Context, id uint64) (*model.MySQLBinlogExportJob, error) {
	var job model.MySQLBinlogExportJob
	err := r.db.GetContext(ctx, &job, `SELECT * FROM mysql_binlog_export_jobs WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &job, err
}

func (r *MySQLBinlogExportRepo) ListQueued(ctx context.Context, limit uint) ([]model.MySQLBinlogExportJob, error) {
	if limit == 0 {
		return []model.MySQLBinlogExportJob{}, nil
	}
	jobs := []model.MySQLBinlogExportJob{}
	err := r.db.SelectContext(ctx, &jobs, `SELECT * FROM mysql_binlog_export_jobs
		WHERE status = 'queued' ORDER BY created_at ASC, id ASC LIMIT ?`, limit)
	return jobs, err
}

func (r *MySQLBinlogExportRepo) ListScoped(ctx context.Context, connectionIDs []uint64, limit, offset int) ([]model.MySQLBinlogExportJob, int64, error) {
	if len(connectionIDs) == 0 {
		return []model.MySQLBinlogExportJob{}, 0, nil
	}
	query, args, err := sqlx.In(`SELECT * FROM mysql_binlog_export_jobs
		WHERE source_connection_id IN (?) ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, connectionIDs, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	jobs := []model.MySQLBinlogExportJob{}
	if err := r.db.SelectContext(ctx, &jobs, r.db.Rebind(query), args...); err != nil {
		return nil, 0, err
	}
	countQuery, countArgs, err := sqlx.In(`SELECT COUNT(*) FROM mysql_binlog_export_jobs WHERE source_connection_id IN (?)`, connectionIDs)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := r.db.GetContext(ctx, &total, r.db.Rebind(countQuery), countArgs...); err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

func (r *MySQLBinlogExportRepo) Claim(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET status = 'running', phase = 'resolving_range', started_at = ?, updated_at = ?
		WHERE id = ? AND status = 'queued'`, now, now, id))
}

func (r *MySQLBinlogExportRepo) UpdatePhase(ctx context.Context, id uint64, phase, message string) (bool, error) {
	return execChanged(r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET phase = ?, progress_message = ?, updated_at = ? WHERE id = ? AND status = 'running'`,
		strings.TrimSpace(phase), nullableTrimmed(message), timeutil.NowUTC(), id))
}

func (r *MySQLBinlogExportRepo) RequestCancel(ctx context.Context, id, actorID uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET phase = CASE WHEN status = 'queued' THEN 'cancelled' ELSE phase END,
		    completed_at = CASE WHEN status = 'queued' THEN ? ELSE completed_at END,
		    status = CASE WHEN status = 'queued' THEN 'cancelled' ELSE 'cancel_requested' END,
		    cancel_requested_by = ?, updated_at = ?
		WHERE id = ? AND status IN ('queued', 'running')`, now, actorID, now, id))
}

func (r *MySQLBinlogExportRepo) MarkCancelled(ctx context.Context, id uint64) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET status = 'cancelled', phase = 'cancelled', completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'cancel_requested'`, now, now, id))
}

func (r *MySQLBinlogExportRepo) MarkFailed(ctx context.Context, id uint64, code, message string) (bool, error) {
	now := timeutil.NowUTC()
	return execChanged(r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET status = 'failed', phase = 'failed', error_code = ?, error_message = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'running'`, nullableTrimmed(code), nullableTrimmed(message), now, now, id))
}

func (r *MySQLBinlogExportRepo) InterruptActive(ctx context.Context) (int64, error) {
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs
		SET status = 'interrupted', phase = 'interrupted', interrupted_at = ?, completed_at = ?, updated_at = ?
		WHERE status IN ('queued', 'running', 'cancel_requested')`, now, now, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *MySQLBinlogExportRepo) Complete(ctx context.Context, id uint64, completion MySQLBinlogExportCompletion, artifacts []MySQLBinlogExportArtifactInput) (bool, error) {
	persistStarted := time.Now()
	encoded, err := r.encodeArtifacts(artifacts)
	if err != nil {
		return false, err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := timeutil.NowUTC()
	for _, artifact := range encoded {
		_, err = tx.ExecContext(ctx, `INSERT INTO mysql_binlog_export_artifacts
			(job_id, artifact_kind, compression, sql_encrypted, plaintext_sha256, plaintext_bytes, compressed_bytes, statement_count, expires_at, created_at)
			VALUES (?, ?, 'gzip', ?, ?, ?, ?, ?, ?, ?)`, id, artifact.Kind, artifact.SQLEncrypted,
			artifact.PlaintextSHA256, artifact.PlaintextBytes, artifact.CompressedBytes, artifact.StatementCount, artifact.ExpiresAt, now)
		if err != nil {
			return false, err
		}
	}
	completion.ArtifactPersistMs = uint64(time.Since(persistStarted).Milliseconds())
	completion.TotalDurationMs += completion.ArtifactPersistMs
	result, err := tx.ExecContext(ctx, `UPDATE mysql_binlog_export_jobs SET
		status = 'succeeded', phase = 'completed', generator = ?, generator_version = ?,
		actual_start_file = ?, actual_start_pos = ?, actual_end_file = ?, actual_end_pos = ?, binlog_file_count = ?,
		queue_wait_ms = ?, range_resolve_ms = ?, forward_generation_ms = ?, rollback_generation_ms = ?,
		artifact_persist_ms = ?, total_duration_ms = ?, artifact_expires_at = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'running'`, completion.Generator, completion.GeneratorVersion,
		completion.ActualStartFile, completion.ActualStartPos, completion.ActualEndFile, completion.ActualEndPos, completion.BinlogFileCount,
		completion.QueueWaitMs, completion.RangeResolveMs, completion.ForwardGenerationMs, completion.RollbackGenerationMs,
		completion.ArtifactPersistMs, completion.TotalDurationMs, artifacts[0].ExpiresAt, now, now, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		if err != nil {
			return false, err
		}
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *MySQLBinlogExportRepo) GetArtifact(ctx context.Context, jobID uint64, kind model.MySQLBinlogExportArtifactKind) (*model.MySQLBinlogExportArtifact, error) {
	var artifact model.MySQLBinlogExportArtifact
	err := r.db.GetContext(ctx, &artifact, `SELECT * FROM mysql_binlog_export_artifacts
		WHERE job_id = ? AND artifact_kind = ? AND purged_at IS NULL`, jobID, kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &artifact, err
}

func (r *MySQLBinlogExportRepo) DecryptArtifact(artifact *model.MySQLBinlogExportArtifact) (string, error) {
	if artifact == nil || artifact.PurgedAt != nil || len(artifact.SQLEncrypted) == 0 {
		return "", errors.New("binlog export artifact is not available")
	}
	if artifact.Compression != "gzip" || artifact.PlaintextBytes > maxBinlogArtifactPlaintextBytes {
		return "", errors.New("binlog export artifact metadata is invalid")
	}
	compressed, err := crypto.Decrypt(r.encKey, artifact.SQLEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt binlog export artifact: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", fmt.Errorf("open binlog export artifact gzip: %w", err)
	}
	defer reader.Close()
	plain, err := io.ReadAll(io.LimitReader(reader, int64(artifact.PlaintextBytes)+1))
	if err != nil {
		return "", fmt.Errorf("decompress binlog export artifact: %w", err)
	}
	if uint64(len(plain)) != artifact.PlaintextBytes {
		return "", errors.New("binlog export artifact size mismatch")
	}
	sum := sha256.Sum256(plain)
	if hex.EncodeToString(sum[:]) != artifact.PlaintextSHA256 {
		return "", errors.New("binlog export artifact checksum mismatch")
	}
	return string(plain), nil
}

func (r *MySQLBinlogExportRepo) PurgeExpired(ctx context.Context, limit uint) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	now := timeutil.NowUTC()
	result, err := r.db.ExecContext(ctx, `UPDATE mysql_binlog_export_artifacts
		SET sql_encrypted = NULL, purged_at = ? WHERE purged_at IS NULL AND expires_at <= ? ORDER BY expires_at ASC LIMIT ?`, now, now, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *MySQLBinlogExportRepo) PurgeMetadata(ctx context.Context, before time.Time, limit uint) (int64, error) {
	if limit == 0 {
		return 0, nil
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM mysql_binlog_export_jobs
		WHERE completed_at IS NOT NULL AND completed_at <= ? AND status IN ('succeeded', 'failed', 'cancelled', 'interrupted')
		ORDER BY completed_at ASC LIMIT ?`, before.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *MySQLBinlogExportRepo) encodeArtifacts(inputs []MySQLBinlogExportArtifactInput) ([]model.MySQLBinlogExportArtifact, error) {
	if len(inputs) != 2 {
		return nil, errors.New("exactly two binlog export artifacts are required")
	}
	seen := make(map[model.MySQLBinlogExportArtifactKind]bool, 2)
	encoded := make([]model.MySQLBinlogExportArtifact, 0, 2)
	for _, input := range inputs {
		if input.Kind != model.MySQLBinlogExportArtifactForwardSQL && input.Kind != model.MySQLBinlogExportArtifactRollbackSQL || seen[input.Kind] {
			return nil, errors.New("one forward and one rollback artifact are required")
		}
		seen[input.Kind] = true
		plain := []byte(input.SQL)
		if len(plain) > maxBinlogArtifactPlaintextBytes {
			return nil, errors.New("binlog export artifact exceeds 25 MiB")
		}
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(plain); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		ciphertext, err := crypto.Encrypt(r.encKey, compressed.Bytes())
		if err != nil {
			return nil, fmt.Errorf("encrypt binlog export artifact: %w", err)
		}
		sum := sha256.Sum256(plain)
		encoded = append(encoded, model.MySQLBinlogExportArtifact{Kind: input.Kind, Compression: "gzip", SQLEncrypted: ciphertext,
			PlaintextSHA256: hex.EncodeToString(sum[:]), PlaintextBytes: uint64(len(plain)), CompressedBytes: uint64(compressed.Len()),
			StatementCount: input.StatementCount, ExpiresAt: input.ExpiresAt})
	}
	if !seen[model.MySQLBinlogExportArtifactForwardSQL] || !seen[model.MySQLBinlogExportArtifactRollbackSQL] {
		return nil, errors.New("one forward and one rollback artifact are required")
	}
	return encoded, nil
}

func execChanged(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
