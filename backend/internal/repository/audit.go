package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type AuditRepo struct {
	db *sqlx.DB
}

func NewAuditRepo(db *sqlx.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

type AuditEntry struct {
	ActorID      *uint64
	ActorName    string
	ActionType   string
	ResourceType string
	ResourceID   *uint64
	Details      any
	IPAddress    string
}

// AuditListFilter holds optional query parameters for listing audit logs.
type AuditListFilter struct {
	ActionType   *string
	ActorID      *uint64
	ActorName    *string
	ResourceType *string
	ResourceID   *uint64
	ResourceName *string
	From         *time.Time
	To           *time.Time
}

// List returns paginated audit log entries matching the filter, plus total count.
func (r *AuditRepo) List(ctx context.Context, f AuditListFilter, limit, offset int) ([]model.AuditLog, int64, error) {
	where := " WHERE 1=1"
	args := []any{}

	if f.ActionType != nil {
		where += " AND action_type = ?"
		args = append(args, *f.ActionType)
	}
	if f.ActorID != nil {
		where += " AND actor_id = ?"
		args = append(args, *f.ActorID)
	}
	if f.ActorName != nil {
		where += " AND actor_name LIKE ?"
		args = append(args, "%"+*f.ActorName+"%")
	}
	if f.ResourceType != nil {
		where += " AND resource_type = ?"
		args = append(args, *f.ResourceType)
	}
	if f.ResourceID != nil {
		where += " AND resource_id = ?"
		args = append(args, *f.ResourceID)
	}
	if f.ResourceName != nil {
		where += " AND JSON_UNQUOTE(JSON_EXTRACT(details, '$.name')) LIKE ?"
		args = append(args, "%"+*f.ResourceName+"%")
	}
	if f.From != nil {
		where += " AND created_at >= ?"
		args = append(args, *f.From)
	}
	if f.To != nil {
		where += " AND created_at <= ?"
		args = append(args, *f.To)
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit_logs count: %w", err)
	}

	listArgs := append(args, limit, offset)
	q := "SELECT id, actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at FROM audit_logs" +
		where + " ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"

	rows, err := r.db.QueryxContext(ctx, q, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit_logs list: %w", err)
	}
	defer rows.Close()

	logs := make([]model.AuditLog, 0, limit)
	for rows.Next() {
		var (
			log          model.AuditLog
			actorName    sql.NullString
			resourceType sql.NullString
			ipAddress    sql.NullString
			details      []byte
		)

		if err := rows.Scan(
			&log.ID,
			&log.ActorID,
			&actorName,
			&log.ActionType,
			&resourceType,
			&log.ResourceID,
			&details,
			&ipAddress,
			&log.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("audit_logs scan: %w", err)
		}

		if actorName.Valid {
			log.ActorName = actorName.String
		}
		if resourceType.Valid {
			value := resourceType.String
			log.ResourceType = &value
		}
		if len(details) > 0 {
			log.Details = json.RawMessage(details)
		}
		if ipAddress.Valid {
			value := ipAddress.String
			log.IPAddress = &value
		}

		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("audit_logs rows: %w", err)
	}

	return logs, total, nil
}

func (r *AuditRepo) Log(ctx context.Context, e AuditEntry) error {
	var detailsJSON []byte
	if e.Details != nil {
		var err error
		detailsJSON, err = json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("marshal audit details: %w", err)
		}
	}

	var ip *string
	if e.IPAddress != "" {
		ip = &e.IPAddress
	}
	var resType *string
	if e.ResourceType != "" {
		resType = &e.ResourceType
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO audit_logs (actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ActorID, e.ActorName, e.ActionType, resType, e.ResourceID, detailsJSON, ip, timeutil.NowUTC(),
	)
	return err
}
