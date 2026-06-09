package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
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
	ResourceType *string
	ResourceID   *uint64
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
	if f.ResourceType != nil {
		where += " AND resource_type = ?"
		args = append(args, *f.ResourceType)
	}
	if f.ResourceID != nil {
		where += " AND resource_id = ?"
		args = append(args, *f.ResourceID)
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
	var logs []model.AuditLog
	q := "SELECT id, actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address, created_at FROM audit_logs" +
		where + " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	if err := r.db.SelectContext(ctx, &logs, q, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("audit_logs list: %w", err)
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
		`INSERT INTO audit_logs (actor_id, actor_name, action_type, resource_type, resource_id, details, ip_address)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ActorID, e.ActorName, e.ActionType, resType, e.ResourceID, detailsJSON, ip,
	)
	return err
}
