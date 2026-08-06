package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

const (
	RollbackStatusUnsupported = "unsupported"
	RollbackStatusGenerating  = "generating"
	RollbackStatusGenerated   = "generated"
	RollbackStatusFailed      = "failed"
	RollbackStatusSubmitted   = "submitted"
)

type TicketRollbackRepo struct {
	db     *sqlx.DB
	encKey []byte
}

type RollbackRange struct {
	StartFile string
	StartPos  uint64
	EndFile   string
	EndPos    uint64
}

type GeneratedRollback struct {
	Generator        string
	GeneratorVersion string
	SQL              string
	StatementCount   int
	Confidence       string
	Warning          string
}

func NewTicketRollbackRepo(db *sqlx.DB, encKey []byte) *TicketRollbackRepo {
	return &TicketRollbackRepo{db: db, encKey: encKey}
}

func (r *TicketRollbackRepo) MarkUnsupported(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution, reason string) error {
	if ticket == nil || ticket.DBConnectionID == nil {
		return nil
	}
	now := timeutil.NowUTC()
	reason = strings.TrimSpace(reason)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_execution_rollbacks
		 (ticket_id, execution_id, seq, status, unsupported_reason, source_connection_id, source_database_name, source_schema_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   status = VALUES(status),
		   unsupported_reason = VALUES(unsupported_reason),
		   failure_message = NULL,
		   updated_at = VALUES(updated_at)`,
		ticket.ID, execRow.ID, execRow.Seq, RollbackStatusUnsupported, nullableTrimmed(reason), *ticket.DBConnectionID, ticket.DatabaseName, ticket.SchemaName, now, now,
	)
	return err
}

func (r *TicketRollbackRepo) MarkGenerating(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution, binlogRange RollbackRange, generator string) error {
	if ticket == nil || ticket.DBConnectionID == nil {
		return nil
	}
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_execution_rollbacks
		 (ticket_id, execution_id, seq, status, generator, source_connection_id, source_database_name, source_schema_name, binlog_start_file, binlog_start_pos, binlog_end_file, binlog_end_pos, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   status = VALUES(status),
		   generator = VALUES(generator),
		   unsupported_reason = NULL,
		   failure_message = NULL,
		   binlog_start_file = VALUES(binlog_start_file),
		   binlog_start_pos = VALUES(binlog_start_pos),
		   binlog_end_file = VALUES(binlog_end_file),
		   binlog_end_pos = VALUES(binlog_end_pos),
		   updated_at = VALUES(updated_at)`,
		ticket.ID, execRow.ID, execRow.Seq, RollbackStatusGenerating, strings.TrimSpace(generator), *ticket.DBConnectionID, ticket.DatabaseName, ticket.SchemaName,
		binlogRange.StartFile, binlogRange.StartPos, binlogRange.EndFile, binlogRange.EndPos, now, now,
	)
	return err
}

func (r *TicketRollbackRepo) MarkGenerated(ctx context.Context, executionID uint64, generated GeneratedRollback) error {
	rollbackSQL := strings.TrimSpace(generated.SQL)
	enc, err := crypto.Encrypt(r.encKey, []byte(rollbackSQL))
	if err != nil {
		return fmt.Errorf("encrypt rollback sql: %w", err)
	}
	sum := sha256.Sum256([]byte(rollbackSQL))
	sha := hex.EncodeToString(sum[:])
	size := int64(len([]byte(rollbackSQL)))
	now := timeutil.NowUTC()
	_, err = r.db.ExecContext(ctx,
		`UPDATE ticket_execution_rollbacks
		 SET status = ?,
		     generator = ?,
		     generator_version = ?,
		     rollback_sql_encrypted = ?,
		     rollback_sql_sha256 = ?,
		     rollback_sql_bytes = ?,
		     statement_count = ?,
		     confidence = ?,
		     warning_message = ?,
		     generated_at = ?,
		     updated_at = ?
		 WHERE execution_id = ?`,
		RollbackStatusGenerated,
		strings.TrimSpace(generated.Generator),
		strings.TrimSpace(generated.GeneratorVersion),
		enc,
		sha,
		size,
		generated.StatementCount,
		nullableTrimmed(generated.Confidence),
		nullableTrimmed(generated.Warning),
		now,
		now,
		executionID,
	)
	return err
}

func (r *TicketRollbackRepo) MarkFailed(ctx context.Context, executionID uint64, message string) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_execution_rollbacks
		 SET status = ?, failure_message = ?, updated_at = ?
		 WHERE execution_id = ?`,
		RollbackStatusFailed, strings.TrimSpace(message), now, executionID,
	)
	return err
}

func (r *TicketRollbackRepo) MarkGenerationFailed(ctx context.Context, ticket *model.Ticket, execRow model.TicketExecution, message string) error {
	if ticket == nil || ticket.DBConnectionID == nil {
		return nil
	}
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_execution_rollbacks
		 (ticket_id, execution_id, seq, status, failure_message, source_connection_id, source_database_name, source_schema_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   status = VALUES(status),
		   failure_message = VALUES(failure_message),
		   updated_at = VALUES(updated_at)`,
		ticket.ID, execRow.ID, execRow.Seq, RollbackStatusFailed, strings.TrimSpace(message), *ticket.DBConnectionID, ticket.DatabaseName, ticket.SchemaName, now, now,
	)
	return err
}

func (r *TicketRollbackRepo) ListByTicket(ctx context.Context, ticketID uint64) ([]model.TicketExecutionRollback, error) {
	items := []model.TicketExecutionRollback{}
	err := r.db.SelectContext(ctx, &items,
		`SELECT * FROM ticket_execution_rollbacks WHERE ticket_id = ? ORDER BY seq ASC, id ASC`,
		ticketID,
	)
	return items, err
}

func (r *TicketRollbackRepo) GetByID(ctx context.Context, id uint64) (*model.TicketExecutionRollback, error) {
	var item model.TicketExecutionRollback
	err := r.db.GetContext(ctx, &item, `SELECT * FROM ticket_execution_rollbacks WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *TicketRollbackRepo) DecryptSQL(item *model.TicketExecutionRollback) (string, error) {
	if item == nil || len(item.RollbackSQLEncrypted) == 0 {
		return "", fmt.Errorf("rollback sql is not available")
	}
	plain, err := crypto.Decrypt(r.encKey, item.RollbackSQLEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt rollback sql: %w", err)
	}
	return string(plain), nil
}

func (r *TicketRollbackRepo) MarkSubmitted(ctx context.Context, rollbackID, rollbackTicketID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_execution_rollbacks SET status = ?, rollback_ticket_id = ?, updated_at = ? WHERE id = ? AND status = ?`,
		RollbackStatusSubmitted, rollbackTicketID, timeutil.NowUTC(), rollbackID, RollbackStatusGenerated,
	)
	return err
}

func nullableTrimmed(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
