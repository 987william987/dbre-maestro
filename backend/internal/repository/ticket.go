package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type TicketRepo struct {
	db *sqlx.DB
}

type TicketListFilter struct {
	SubmitterID           *uint64
	VisibleToUserID       *uint64
	VisibleToAllTickets   bool
	VisibleToExecutorPool bool
	ReviewWorkflowTypes   []model.ApprovalWorkflowType
	Status                *model.TicketStatus
	Type                  *model.TicketType
	Keyword               *string
	From                  *time.Time
	To                    *time.Time
}

type WorkflowDashboardSummary struct {
	NormalExports       int64                         `json:"normal_exports"`
	SensitiveExports    int64                         `json:"sensitive_exports"`
	AutoApprovedExports int64                         `json:"auto_approved_exports"`
	NeedsAdminAttention int64                         `json:"needs_admin_attention"`
	ByType              []WorkflowDashboardCount      `json:"by_type"`
	BySubmitter         []WorkflowDashboardUserCount  `json:"by_submitter"`
	ByReviewer          []WorkflowDashboardUserCount  `json:"by_reviewer"`
	ByExecutor          []WorkflowDashboardUserCount  `json:"by_executor"`
	ByWorkflowError     []WorkflowDashboardErrorCount `json:"by_workflow_error"`
}

type WorkflowDashboardCount struct {
	Key   string `db:"key_name" json:"key"`
	Count int64  `db:"count" json:"count"`
}

type WorkflowDashboardUserCount struct {
	UserID   *uint64 `db:"user_id" json:"user_id,omitempty"`
	Username *string `db:"username" json:"username,omitempty"`
	Count    int64   `db:"count" json:"count"`
}

type WorkflowDashboardErrorCount struct {
	ErrorCode string `db:"error_code" json:"error_code"`
	Count     int64  `db:"count" json:"count"`
}

func NewTicketRepo(db *sqlx.DB) *TicketRepo {
	return &TicketRepo{db: db}
}

func (r *TicketRepo) Create(ctx context.Context, t *model.Ticket) (*model.Ticket, error) {
	return r.CreateWithScopes(ctx, t, nil)
}

func (r *TicketRepo) CreateWithScopes(ctx context.Context, t *model.Ticket, scopes []model.TicketScope) (*model.Ticket, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create ticket tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	ticketNo, err := generateTicketNo()
	if err != nil {
		return nil, fmt.Errorf("generate ticket number: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tickets (ticket_no, title, description, sql_content, ticket_type, contains_sensitive, db_connection_id, database_name, status, submitter_id, approved_duration_minutes, approved_until, revoked_at, revoked_by, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending_review', ?, ?, ?, ?, ?, ?, ?)`,
		ticketNo, t.Title, t.Description, t.SQLContent, t.TicketType, t.ContainsSensitive, t.DBConnectionID, t.DatabaseName, t.SubmitterID,
		t.ApprovedDurationMinutes, t.ApprovedUntil, t.RevokedAt, t.RevokedBy, timeutil.NowUTC(), timeutil.NowUTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("create ticket: %w", err)
	}
	id, _ := res.LastInsertId()
	if len(scopes) > 0 {
		for _, scope := range scopes {
			if strings.TrimSpace(scope.ColumnName) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO ticket_scopes (ticket_id, connection_id, database_name, schema_name, table_name, column_name, is_sensitive, source_kind, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, scope.ConnectionID, scope.DatabaseName, scope.SchemaName, scope.TableName, scope.ColumnName, scope.IsSensitive, scope.SourceKind, timeutil.NowUTC(),
			); err != nil {
				return nil, fmt.Errorf("create ticket scope: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create ticket tx: %w", err)
	}
	tx = nil
	return r.GetByID(ctx, uint64(id))
}

func generateTicketNo() (string, error) {
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}

	timestamp := timeutil.NowUTC().Format("20060102-150405000")
	return fmt.Sprintf("TK-%s-%s", timestamp, strings.ToUpper(hex.EncodeToString(suffix[:]))), nil
}

func (r *TicketRepo) GetByID(ctx context.Context, id uint64) (*model.Ticket, error) {
	var t model.Ticket
	err := r.db.GetContext(ctx, &t, `SELECT * FROM tickets WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

func (r *TicketRepo) GetByTicketNo(ctx context.Context, ticketNo string) (*model.Ticket, error) {
	var t model.Ticket
	err := r.db.GetContext(ctx, &t, `SELECT * FROM tickets WHERE ticket_no = ?`, ticketNo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &t, err
}

func (r *TicketRepo) SaveWorkflowSnapshot(ctx context.Context, ticketID uint64, resolution *model.WorkflowResolution) error {
	if resolution == nil {
		resolution = &model.WorkflowResolution{}
	}
	approvalUserIDs, err := json.Marshal(resolution.ApprovalUserIDs)
	if err != nil {
		return fmt.Errorf("encode workflow approval users: %w", err)
	}
	executorUserIDs, err := json.Marshal(resolution.ExecutorUserIDs)
	if err != nil {
		return fmt.Errorf("encode workflow executor users: %w", err)
	}
	adminUserIDs, err := json.Marshal(resolution.AdminUserIDs)
	if err != nil {
		return fmt.Errorf("encode workflow admin users: %w", err)
	}
	resolutionTrace, err := json.Marshal(resolution)
	if err != nil {
		return fmt.Errorf("encode workflow resolution trace: %w", err)
	}
	now := timeutil.NowUTC()
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_workflow_snapshots
		 (ticket_id, workflow_rule_id, workflow_rule_name, approval_enabled, approval_user_ids, executor_user_ids, admin_user_ids, error_code, error_message, resolution_trace, resolved_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   workflow_rule_id = VALUES(workflow_rule_id),
		   workflow_rule_name = VALUES(workflow_rule_name),
		   approval_enabled = VALUES(approval_enabled),
		   approval_user_ids = VALUES(approval_user_ids),
		   executor_user_ids = VALUES(executor_user_ids),
		   admin_user_ids = VALUES(admin_user_ids),
		   error_code = VALUES(error_code),
		   error_message = VALUES(error_message),
		   resolution_trace = VALUES(resolution_trace),
		   resolved_at = VALUES(resolved_at),
		   updated_at = VALUES(updated_at)`,
		ticketID, resolution.RuleID, resolution.RuleName, resolution.ApprovalEnabled, string(approvalUserIDs), string(executorUserIDs),
		string(adminUserIDs), resolution.ErrorCode, resolution.ErrorMessage, string(resolutionTrace), now, now, now,
	); err != nil {
		return fmt.Errorf("save ticket workflow snapshot: %w", err)
	}
	return nil
}

func (r *TicketRepo) GetWorkflowSnapshot(ctx context.Context, ticketID uint64) (*model.TicketWorkflowSnapshot, error) {
	var row struct {
		TicketID        uint64    `db:"ticket_id"`
		RuleID          *uint64   `db:"workflow_rule_id"`
		RuleName        string    `db:"workflow_rule_name"`
		ApprovalEnabled bool      `db:"approval_enabled"`
		ApprovalUserIDs string    `db:"approval_user_ids"`
		ExecutorUserIDs string    `db:"executor_user_ids"`
		AdminUserIDs    string    `db:"admin_user_ids"`
		ErrorCode       string    `db:"error_code"`
		ErrorMessage    string    `db:"error_message"`
		ResolutionTrace string    `db:"resolution_trace"`
		ResolvedAt      time.Time `db:"resolved_at"`
		CreatedAt       time.Time `db:"created_at"`
		UpdatedAt       time.Time `db:"updated_at"`
	}
	err := r.db.GetContext(ctx, &row,
		`SELECT ticket_id, workflow_rule_id, workflow_rule_name, approval_enabled,
		        approval_user_ids, executor_user_ids, admin_user_ids, error_code, error_message, resolution_trace,
		        resolved_at, created_at, updated_at
		 FROM ticket_workflow_snapshots
		 WHERE ticket_id = ?`,
		ticketID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ticket workflow snapshot: %w", err)
	}
	snapshot := &model.TicketWorkflowSnapshot{
		TicketID:        row.TicketID,
		RuleID:          row.RuleID,
		RuleName:        row.RuleName,
		ApprovalEnabled: row.ApprovalEnabled,
		ErrorCode:       row.ErrorCode,
		ErrorMessage:    row.ErrorMessage,
		ResolutionTrace: row.ResolutionTrace,
		ResolvedAt:      row.ResolvedAt,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(row.ApprovalUserIDs), &snapshot.ApprovalUserIDs); err != nil {
		return nil, fmt.Errorf("decode workflow approval users: %w", err)
	}
	if err := json.Unmarshal([]byte(row.ExecutorUserIDs), &snapshot.ExecutorUserIDs); err != nil {
		return nil, fmt.Errorf("decode workflow executor users: %w", err)
	}
	if err := json.Unmarshal([]byte(row.AdminUserIDs), &snapshot.AdminUserIDs); err != nil {
		return nil, fmt.Errorf("decode workflow admin users: %w", err)
	}
	return snapshot, nil
}

func (r *TicketRepo) Delete(ctx context.Context, id uint64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM tickets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete ticket: %w", err)
	}
	return nil
}

func (r *TicketRepo) ListScopes(ctx context.Context, ticketID uint64) ([]model.TicketScope, error) {
	scopes := []model.TicketScope{}
	if err := r.db.SelectContext(ctx, &scopes,
		`SELECT id, ticket_id, connection_id, database_name, schema_name, table_name, column_name, is_sensitive, source_kind, created_at
		 FROM ticket_scopes
		 WHERE ticket_id = ?
		 ORDER BY id ASC`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("list ticket scopes: %w", err)
	}
	return scopes, nil
}

func (r *TicketRepo) List(ctx context.Context, filter TicketListFilter, limit, offset int) ([]model.Ticket, int64, error) {
	where := ` WHERE 1=1`
	args := []any{}

	if filter.SubmitterID != nil {
		where += ` AND t.submitter_id = ?`
		args = append(args, *filter.SubmitterID)
	}
	if filter.VisibleToUserID != nil && !filter.VisibleToAllTickets {
		visibility := []string{`t.submitter_id = ?`}
		visibilityArgs := []any{*filter.VisibleToUserID}
		participantIDJSON := strconv.FormatUint(*filter.VisibleToUserID, 10)
		visibility = append(visibility, `EXISTS (
			SELECT 1
			FROM ticket_workflow_snapshots tws
			WHERE tws.ticket_id = t.id
			  AND (
			    JSON_CONTAINS(tws.approval_user_ids, ?)
			    OR JSON_CONTAINS(tws.admin_user_ids, ?)
			    OR (t.status = ? AND JSON_CONTAINS(tws.executor_user_ids, ?))
			  )
		)`)
		visibilityArgs = append(visibilityArgs, participantIDJSON, participantIDJSON, model.TicketStatusPendingExecution, participantIDJSON)
		for _, workflowType := range filter.ReviewWorkflowTypes {
			condition, conditionArgs := ticketWorkflowCondition(workflowType)
			if condition == "" {
				continue
			}
			visibility = append(visibility, condition)
			visibilityArgs = append(visibilityArgs, conditionArgs...)
		}
		if filter.VisibleToExecutorPool {
			visibility = append(visibility, `(t.ticket_type IN (?, ?, ?) AND t.status = ?)`)
			visibilityArgs = append(visibilityArgs, model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeRedisCommand, model.TicketStatusPendingExecution)
		}
		where += ` AND (` + strings.Join(visibility, ` OR `) + `)`
		args = append(args, visibilityArgs...)
	}
	if filter.Status != nil {
		where += ` AND t.status = ?`
		args = append(args, *filter.Status)
	}
	if filter.Type != nil {
		where += ` AND t.ticket_type = ?`
		args = append(args, *filter.Type)
	}
	if filter.Keyword != nil && strings.TrimSpace(*filter.Keyword) != "" {
		keyword := "%" + strings.TrimSpace(*filter.Keyword) + "%"
		where += ` AND (t.ticket_no LIKE ? OR t.title LIKE ? OR u.username LIKE ?)`
		args = append(args, keyword, keyword, keyword)
	}
	if filter.From != nil {
		where += ` AND t.created_at >= ?`
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		where += ` AND t.created_at <= ?`
		args = append(args, *filter.To)
	}

	countQuery := `SELECT COUNT(*) FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id` + where
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tickets: %w", err)
	}

	query := `SELECT t.* FROM tickets t
		LEFT JOIN users u ON u.id = t.submitter_id` + where + ` ORDER BY t.created_at DESC LIMIT ? OFFSET ?`
	listArgs := append(args, limit, offset)

	var tickets []model.Ticket
	err := r.db.SelectContext(ctx, &tickets, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

func (r *TicketRepo) WorkflowDashboardSummary(ctx context.Context) (*WorkflowDashboardSummary, error) {
	summary := &WorkflowDashboardSummary{}
	if err := r.db.GetContext(ctx, &summary.NormalExports,
		`SELECT COUNT(*) FROM tickets WHERE ticket_type = ? AND COALESCE(contains_sensitive, 0) = 0`,
		model.TicketTypeSQLExport,
	); err != nil {
		return nil, fmt.Errorf("count normal exports: %w", err)
	}
	if err := r.db.GetContext(ctx, &summary.SensitiveExports,
		`SELECT COUNT(*) FROM tickets WHERE ticket_type = ? AND contains_sensitive = 1`,
		model.TicketTypeSQLExport,
	); err != nil {
		return nil, fmt.Errorf("count sensitive exports: %w", err)
	}
	if err := r.db.GetContext(ctx, &summary.AutoApprovedExports,
		`SELECT COUNT(*) FROM tickets t
		 JOIN ticket_workflow_snapshots tws ON tws.ticket_id = t.id
		 WHERE t.ticket_type = ? AND COALESCE(t.contains_sensitive, 0) = 0 AND tws.approval_enabled = 0`,
		model.TicketTypeSQLExport,
	); err != nil {
		return nil, fmt.Errorf("count auto-approved exports: %w", err)
	}
	if err := r.db.GetContext(ctx, &summary.NeedsAdminAttention,
		`SELECT COUNT(*) FROM tickets WHERE status = ?`,
		model.TicketStatusNeedsAdminAttention,
	); err != nil {
		return nil, fmt.Errorf("count needs admin attention: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByType,
		`SELECT ticket_type AS key_name, COUNT(*) AS count FROM tickets GROUP BY ticket_type ORDER BY count DESC`,
	); err != nil {
		return nil, fmt.Errorf("count by type: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.BySubmitter,
		`SELECT t.submitter_id AS user_id, u.username AS username, COUNT(*) AS count
		 FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id
		 GROUP BY t.submitter_id, u.username ORDER BY count DESC LIMIT 20`,
	); err != nil {
		return nil, fmt.Errorf("count by submitter: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByReviewer,
		`SELECT t.reviewer_id AS user_id, u.username AS username, COUNT(*) AS count
		 FROM tickets t LEFT JOIN users u ON u.id = t.reviewer_id
		 WHERE t.reviewer_id IS NOT NULL
		 GROUP BY t.reviewer_id, u.username ORDER BY count DESC LIMIT 20`,
	); err != nil {
		return nil, fmt.Errorf("count by reviewer: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByExecutor,
		`SELECT t.executor_id AS user_id, u.username AS username, COUNT(*) AS count
		 FROM tickets t LEFT JOIN users u ON u.id = t.executor_id
		 WHERE t.executor_id IS NOT NULL
		 GROUP BY t.executor_id, u.username ORDER BY count DESC LIMIT 20`,
	); err != nil {
		return nil, fmt.Errorf("count by executor: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByWorkflowError,
		`SELECT error_code, COUNT(*) AS count
		 FROM ticket_workflow_snapshots
		 WHERE error_code <> ''
		 GROUP BY error_code ORDER BY count DESC`,
	); err != nil {
		return nil, fmt.Errorf("count workflow errors: %w", err)
	}
	return summary, nil
}

func ticketWorkflowCondition(workflowType model.ApprovalWorkflowType) (string, []any) {
	switch workflowType {
	case model.ApprovalWorkflowDDL:
		return `t.ticket_type = ?`, []any{model.TicketTypeDDL}
	case model.ApprovalWorkflowDML:
		return `t.ticket_type = ?`, []any{model.TicketTypeDML}
	case model.ApprovalWorkflowRedisCommand:
		return `t.ticket_type = ?`, []any{model.TicketTypeRedisCommand}
	case model.ApprovalWorkflowQueryAccess:
		return `t.ticket_type = ?`, []any{model.TicketTypeQueryAccess}
	case model.ApprovalWorkflowSensitiveQueryAccess:
		return `t.ticket_type = ?`, []any{model.TicketTypeSensitiveQueryAccess}
	case model.ApprovalWorkflowSQLExportNormal:
		return `(t.ticket_type = ? AND (t.contains_sensitive IS NULL OR t.contains_sensitive = 0))`, []any{model.TicketTypeSQLExport}
	case model.ApprovalWorkflowSQLExportSensitive:
		return `(t.ticket_type = ? AND t.contains_sensitive = 1)`, []any{model.TicketTypeSQLExport}
	default:
		return "", nil
	}
}

func (r *TicketRepo) UpdateStatus(ctx context.Context, id uint64, fromStatus, toStatus model.TicketStatus, reviewerID *uint64, comment *string, rejectionReason *string) (bool, error) {
	query := `UPDATE tickets SET status = ?, updated_at = ?`
	args := []any{toStatus, timeutil.NowUTC()}

	if reviewerID != nil {
		query += `, reviewer_id = ?`
		args = append(args, *reviewerID)
	}
	if comment != nil {
		query += `, review_comment = ?`
		args = append(args, *comment)
	}
	if rejectionReason != nil {
		query += `, rejection_reason = ?`
		args = append(args, *rejectionReason)
	}

	query += ` WHERE id = ? AND status = ?`
	args = append(args, id, fromStatus)

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("update ticket status: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *TicketRepo) ApproveSensitiveAccess(ctx context.Context, id uint64, fromStatus model.TicketStatus, reviewerID uint64, approvedUntil time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, reviewer_id = ?, approved_until = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND ticket_type = ?`,
		model.TicketStatusApproved, reviewerID, approvedUntil.UTC(), timeutil.NowUTC(), id, fromStatus, model.TicketTypeSensitiveQueryAccess,
	)
	if err != nil {
		return false, fmt.Errorf("approve sensitive access: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *TicketRepo) RevokeSensitiveAccess(ctx context.Context, id uint64, actorID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE id = ? AND ticket_type = ? AND status = ? AND revoked_at IS NULL`,
		model.TicketStatusStopped, timeutil.NowUTC(), actorID, timeutil.NowUTC(), id, model.TicketTypeSensitiveQueryAccess, model.TicketStatusApproved,
	)
	if err != nil {
		return false, fmt.Errorf("revoke sensitive access: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *TicketRepo) ListActiveSensitiveAccessScopes(ctx context.Context, userID, connectionID uint64) ([]model.TicketScope, error) {
	scopes := []model.TicketScope{}
	if err := r.db.SelectContext(ctx, &scopes,
		`SELECT ts.id, ts.ticket_id, ts.connection_id, ts.database_name, ts.schema_name, ts.table_name, ts.column_name, ts.is_sensitive, ts.source_kind, ts.created_at
		 FROM ticket_scopes ts
		 JOIN tickets t ON t.id = ts.ticket_id
		 WHERE t.submitter_id = ?
		   AND t.ticket_type = ?
		   AND t.status = ?
		   AND t.approved_until IS NOT NULL
		   AND t.approved_until > ?
		   AND t.revoked_at IS NULL
		   AND ts.connection_id = ?
		 ORDER BY ts.id ASC`,
		userID, model.TicketTypeSensitiveQueryAccess, model.TicketStatusApproved, timeutil.NowUTC(), connectionID,
	); err != nil {
		return nil, fmt.Errorf("list active sensitive access scopes: %w", err)
	}
	return scopes, nil
}

// T9: OCC — atomically transition to executing, returns false if already taken
func (r *TicketRepo) StartExecution(ctx context.Context, id, executorID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'executing', executor_id = ?, started_at = ?, updated_at = ?
         WHERE id = ? AND status = 'pending_execution'`,
		executorID, timeutil.NowUTC(), timeutil.NowUTC(), id,
	)
	if err != nil {
		return false, fmt.Errorf("start execution OCC: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *TicketRepo) MarkCompleted(ctx context.Context, id uint64, status model.TicketStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		status, timeutil.NowUTC(), timeutil.NowUTC(), id,
	)
	return err
}

func (r *TicketRepo) CreateExecution(ctx context.Context, e *model.TicketExecution) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO ticket_executions (ticket_id, seq, sql_stmt, status, started_at) VALUES (?, ?, ?, 'pending', NULL)`,
		e.TicketID, e.Seq, e.SQLStmt,
	)
	if err != nil {
		return 0, fmt.Errorf("create ticket_execution: %w", err)
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (r *TicketRepo) MarkExecutionRunning(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions SET status = 'running', started_at = ? WHERE id = ?`,
		timeutil.NowUTC(), id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionDone(ctx context.Context, id uint64, rowsAffected *int64, durationMs int64, errMsg *string) error {
	status := "completed"
	if errMsg != nil {
		status = "failed"
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions SET status = ?, rows_affected = ?, error_msg = ?, completed_at = ?, duration_ms = ? WHERE id = ?`,
		status, rowsAffected, errMsg, timeutil.NowUTC(), durationMs, id,
	)
	return err
}

// ListExecutions returns all execution rows for a ticket, ordered by seq.
func (r *TicketRepo) ListExecutions(ctx context.Context, ticketID uint64) ([]model.TicketExecution, error) {
	var execs []model.TicketExecution
	err := r.db.SelectContext(ctx, &execs,
		`SELECT * FROM ticket_executions WHERE ticket_id = ? ORDER BY seq`,
		ticketID,
	)
	return execs, err
}

func (r *TicketRepo) ReplaceReviewResults(ctx context.Context, ticketID uint64, results []model.TicketReviewResult) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace review results tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM ticket_review_results WHERE ticket_id = ?`, ticketID); err != nil {
		return fmt.Errorf("delete ticket review results: %w", err)
	}
	for _, result := range results {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ticket_review_results
			 (ticket_id, seq, sql_stmt, phase, validation_stage, statement_kind, object_type, validation_method, scan_rows, status, message, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ticketID,
			result.Seq,
			result.SQLStmt,
			result.Phase,
			result.ValidationStage,
			result.StatementKind,
			result.ObjectType,
			result.ValidationMethod,
			result.ScanRows,
			result.Status,
			result.Message,
			timeutil.NowUTC(),
		); err != nil {
			return fmt.Errorf("insert ticket review result: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace review results: %w", err)
	}
	tx = nil
	return nil
}

func (r *TicketRepo) ListReviewResults(ctx context.Context, ticketID uint64) ([]model.TicketReviewResult, error) {
	results := []model.TicketReviewResult{}
	if err := r.db.SelectContext(ctx, &results,
		`SELECT id, ticket_id, seq, sql_stmt, phase, validation_stage, statement_kind, object_type, validation_method, scan_rows, status, message, created_at
		 FROM ticket_review_results
		 WHERE ticket_id = ?
		 ORDER BY seq ASC, id ASC`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("list ticket review results: %w", err)
	}
	return results, nil
}

func (r *TicketRepo) DB() *sqlx.DB {
	return r.db
}

// MarkStopped transitions an executing ticket to stopped.
// Returns false if the ticket was not in executing state (idempotent).
func (r *TicketRepo) MarkStopped(ctx context.Context, id uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'stopped', updated_at = ? WHERE id = ? AND status = 'executing'`,
		timeutil.NowUTC(), id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetScheduled stores executor_id and scheduled_at for deferred execution.
// The ticket must be in pending_execution status.
func (r *TicketRepo) SetScheduled(ctx context.Context, id, executorID uint64, scheduledAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET executor_id = ?, scheduled_at = ?, updated_at = ? WHERE id = ? AND status = 'pending_execution'`,
		executorID, scheduledAt.UTC(), timeutil.NowUTC(), id,
	)
	return err
}

// GetDueScheduled returns pending_execution tickets whose scheduled_at has arrived.
func (r *TicketRepo) GetDueScheduled(ctx context.Context) ([]model.Ticket, error) {
	var tickets []model.Ticket
	err := r.db.SelectContext(ctx, &tickets,
		`SELECT * FROM tickets WHERE status = 'pending_execution' AND scheduled_at IS NOT NULL AND scheduled_at <= ?`,
		timeutil.NowUTC(),
	)
	return tickets, err
}

// Crash recovery: on startup, scan executing → mark interrupted
func (r *TicketRepo) MarkInterruptedAll(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = 'interrupted', updated_at = ? WHERE status = 'executing'`,
		timeutil.NowUTC(),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
