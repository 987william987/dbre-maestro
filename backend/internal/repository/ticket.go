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
	TicketNo              *string
	Title                 *string
	Submitter             *string
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

type TicketExecutionRecovery struct {
	TicketID           uint64
	Status             model.TicketStatus
	Reason             string
	FailedExecutionIDs []uint64
	FailedExecutions   []TicketExecutionRecoveryDetail
}

type TicketExecutionRecoveryDetail struct {
	ExecutionID        uint64     `json:"execution_id"`
	Seq                int        `json:"seq"`
	SentToDBAt         *time.Time `json:"sent_to_db_at,omitempty"`
	DBProcessType      *string    `json:"db_process_type,omitempty"`
	DBProcessID        *uint64    `json:"db_process_id,omitempty"`
	InterruptionReason string     `json:"interruption_reason"`
	OutcomeConfidence  string     `json:"outcome_confidence"`
}

type TicketTodoSummary struct {
	Pending           int64 `json:"pending"`
	ReviewRequired    int64 `json:"review_required"`
	ExecutionRequired int64 `json:"execution_required"`
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

type PlatformQueueStats struct {
	PendingReview       int64 `db:"pending_review" json:"pending_review"`
	PendingExecution    int64 `db:"pending_execution" json:"pending_execution"`
	Executing           int64 `db:"executing" json:"executing"`
	NeedsAdminAttention int64 `db:"needs_admin_attention" json:"needs_admin_attention"`
	FailedToday         int64 `db:"failed_today" json:"failed_today"`
	Failed7d            int64 `db:"failed_7d" json:"failed_7d"`
	LongPending         int64 `db:"long_pending" json:"long_pending"`
}

type PlatformAgingStats struct {
	AvgReviewAgeMinutes        *float64 `json:"avg_review_age_minutes,omitempty"`
	AvgExecutionWaitAgeMinutes *float64 `json:"avg_execution_wait_age_minutes,omitempty"`
	AvgExecutionDurationMs     *float64 `json:"avg_execution_duration_ms,omitempty"`
	MaxReviewAgeMinutes        *int64   `json:"max_review_age_minutes,omitempty"`
	MaxExecutionWaitAgeMinutes *int64   `json:"max_execution_wait_age_minutes,omitempty"`
}

type PlatformExecutionRiskStats struct {
	RecentFailed    int64                    `db:"recent_failed" json:"recent_failed"`
	ManuallyStopped int64                    `db:"manually_stopped" json:"manually_stopped"`
	ServiceShutdown int64                    `db:"service_shutdown" json:"service_shutdown"`
	OutcomeUnknown  int64                    `db:"outcome_unknown" json:"outcome_unknown"`
	NotSent         int64                    `db:"not_sent" json:"not_sent"`
	DBExplicitError int64                    `db:"db_explicit_error" json:"db_explicit_error"`
	ByOutcome       []WorkflowDashboardCount `json:"by_outcome"`
	ByInterruption  []WorkflowDashboardCount `json:"by_interruption"`
}

type PlatformTopUsageStats struct {
	Submitters             []WorkflowDashboardUserCount `json:"submitters"`
	DBConnectionsByTickets []WorkflowDashboardCount     `json:"db_connections_by_tickets"`
	FailedDBConnections    []WorkflowDashboardCount     `json:"failed_db_connections"`
	SQLExportsByUser       []WorkflowDashboardUserCount `json:"sql_exports_by_user"`
}

type TicketDashboardSummary struct {
	Total    int64                    `json:"total"`
	ByType   []WorkflowDashboardCount `json:"by_type"`
	ByStatus []WorkflowDashboardCount `json:"by_status"`
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
		`INSERT INTO tickets (ticket_no, title, description, sql_content, ticket_type, contains_sensitive, db_connection_id, database_name, schema_name, status, submitter_id, approved_duration_minutes, approved_until, revoked_at, revoked_by, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending_review', ?, ?, ?, ?, ?, ?, ?)`,
		ticketNo, t.Title, t.Description, t.SQLContent, t.TicketType, t.ContainsSensitive, t.DBConnectionID, t.DatabaseName, t.SchemaName, t.SubmitterID,
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
		 (ticket_id, workflow_rule_id, workflow_rule_name, approval_enabled, execution_mode, approval_user_ids, executor_user_ids, admin_user_ids, error_code, error_message, resolution_trace, resolved_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE
		   workflow_rule_id = VALUES(workflow_rule_id),
		   workflow_rule_name = VALUES(workflow_rule_name),
		   approval_enabled = VALUES(approval_enabled),
		   execution_mode = VALUES(execution_mode),
		   approval_user_ids = VALUES(approval_user_ids),
		   executor_user_ids = VALUES(executor_user_ids),
		   admin_user_ids = VALUES(admin_user_ids),
		   error_code = VALUES(error_code),
		   error_message = VALUES(error_message),
		   resolution_trace = VALUES(resolution_trace),
		   resolved_at = VALUES(resolved_at),
		   updated_at = VALUES(updated_at)`,
		ticketID, resolution.RuleID, resolution.RuleName, resolution.ApprovalEnabled, normalizeWorkflowExecutionMode(resolution.ExecutionMode), string(approvalUserIDs), string(executorUserIDs),
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
		ExecutionMode   string    `db:"execution_mode"`
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
		`SELECT ticket_id, workflow_rule_id, workflow_rule_name, approval_enabled, execution_mode,
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
		ExecutionMode:   normalizeWorkflowExecutionMode(row.ExecutionMode),
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
	if filter.TicketNo != nil && strings.TrimSpace(*filter.TicketNo) != "" {
		ticketNo := "%" + strings.TrimSpace(*filter.TicketNo) + "%"
		where += ` AND t.ticket_no LIKE ?`
		args = append(args, ticketNo)
	}
	if filter.Title != nil && strings.TrimSpace(*filter.Title) != "" {
		title := "%" + strings.TrimSpace(*filter.Title) + "%"
		where += ` AND t.title LIKE ?`
		args = append(args, title)
	}
	if filter.Submitter != nil && strings.TrimSpace(*filter.Submitter) != "" {
		submitter := "%" + strings.TrimSpace(*filter.Submitter) + "%"
		where += ` AND u.username LIKE ?`
		args = append(args, submitter)
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
		LEFT JOIN users u ON u.id = t.submitter_id` + where + ` ORDER BY CASE WHEN t.status IN (?, ?, ?) THEN 0 ELSE 1 END, t.created_at DESC LIMIT ? OFFSET ?`
	args = append(args, model.TicketStatusPendingReview, model.TicketStatusPendingExecution, model.TicketStatusNeedsAdminAttention)
	listArgs := append(args, limit, offset)

	var tickets []model.Ticket
	err := r.db.SelectContext(ctx, &tickets, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	return tickets, total, nil
}

func (r *TicketRepo) TodoSummary(ctx context.Context, userID uint64, canReview bool, canExecute bool) (*TicketTodoSummary, error) {
	summary := &TicketTodoSummary{}
	activeSubmitterStatuses := []model.TicketStatus{
		model.TicketStatusPendingReview,
		model.TicketStatusPendingExecution,
	}
	query, args, err := sqlx.In(
		`SELECT COUNT(*) FROM tickets WHERE submitter_id = ? AND status IN (?)`,
		userID,
		activeSubmitterStatuses,
	)
	if err != nil {
		return nil, fmt.Errorf("build pending ticket count query: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, r.db.Rebind(query), args...).Scan(&summary.Pending); err != nil {
		return nil, fmt.Errorf("count pending tickets: %w", err)
	}

	participantIDJSON := strconv.FormatUint(userID, 10)
	if canReview {
		if err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*)
			 FROM tickets t
			 JOIN ticket_workflow_snapshots tws ON tws.ticket_id = t.id
			 WHERE t.status = ?
			   AND t.submitter_id <> ?
			   AND tws.approval_enabled = 1
			   AND COALESCE(tws.error_code, '') = ''
			   AND JSON_CONTAINS(tws.approval_user_ids, ?)`,
			model.TicketStatusPendingReview, userID, participantIDJSON,
		).Scan(&summary.ReviewRequired); err != nil {
			return nil, fmt.Errorf("count review required tickets: %w", err)
		}
	}

	if canExecute {
		if err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*)
			 FROM tickets t
			 JOIN ticket_workflow_snapshots tws ON tws.ticket_id = t.id
			 WHERE t.status = ?
			   AND t.submitter_id <> ?
			   AND (t.reviewer_id IS NULL OR t.reviewer_id <> ?)
			   AND COALESCE(tws.error_code, '') = ''
			   AND JSON_CONTAINS(tws.executor_user_ids, ?)`,
			model.TicketStatusPendingExecution, userID, userID, participantIDJSON,
		).Scan(&summary.ExecutionRequired); err != nil {
			return nil, fmt.Errorf("count execution required tickets: %w", err)
		}
	}
	return summary, nil
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

func (r *TicketRepo) TicketDashboardSummary(ctx context.Context, submitterID *uint64) (*TicketDashboardSummary, error) {
	summary := &TicketDashboardSummary{}
	where := ""
	args := []any{}
	if submitterID != nil {
		where = " WHERE submitter_id = ?"
		args = append(args, *submitterID)
	}
	if err := r.db.GetContext(ctx, &summary.Total, `SELECT COUNT(*) FROM tickets`+where, args...); err != nil {
		return nil, fmt.Errorf("count dashboard tickets: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByType, `SELECT ticket_type AS key_name, COUNT(*) AS count FROM tickets`+where+` GROUP BY ticket_type ORDER BY count DESC`, args...); err != nil {
		return nil, fmt.Errorf("count dashboard tickets by type: %w", err)
	}
	if err := r.db.SelectContext(ctx, &summary.ByStatus, `SELECT status AS key_name, COUNT(*) AS count FROM tickets`+where+` GROUP BY status ORDER BY count DESC`, args...); err != nil {
		return nil, fmt.Errorf("count dashboard tickets by status: %w", err)
	}
	return summary, nil
}

func (r *TicketRepo) RecentTicketsBySubmitter(ctx context.Context, submitterID uint64, limit int) ([]model.Ticket, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var tickets []model.Ticket
	if err := r.db.SelectContext(ctx, &tickets,
		`SELECT * FROM tickets WHERE submitter_id = ? ORDER BY CASE WHEN status IN (?, ?, ?, ?) THEN 0 ELSE 1 END, updated_at DESC LIMIT ?`,
		submitterID,
		model.TicketStatusPendingReview,
		model.TicketStatusPendingExecution,
		model.TicketStatusExecuting,
		model.TicketStatusNeedsAdminAttention,
		limit,
	); err != nil {
		return nil, fmt.Errorf("list recent submitter tickets: %w", err)
	}
	return tickets, nil
}

func (r *TicketRepo) ActiveTicketsBySubmitter(ctx context.Context, submitterID uint64, limit int) ([]model.Ticket, error) {
	if limit <= 0 || limit > 20 {
		limit = 6
	}
	statuses := []model.TicketStatus{
		model.TicketStatusPendingReview,
		model.TicketStatusPendingExecution,
		model.TicketStatusExecuting,
		model.TicketStatusNeedsAdminAttention,
	}
	query, args, err := sqlx.In(
		`SELECT * FROM tickets WHERE submitter_id = ? AND status IN (?) ORDER BY updated_at DESC LIMIT ?`,
		submitterID,
		statuses,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("build active submitter tickets query: %w", err)
	}
	query = r.db.Rebind(query)
	var tickets []model.Ticket
	if err := r.db.SelectContext(ctx, &tickets, query, args...); err != nil {
		return nil, fmt.Errorf("list active submitter tickets: %w", err)
	}
	return tickets, nil
}

func (r *TicketRepo) RecentPlatformAttentionTickets(ctx context.Context, limit int) ([]model.Ticket, error) {
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	statuses := []model.TicketStatus{
		model.TicketStatusNeedsAdminAttention,
		model.TicketStatusExecuting,
		model.TicketStatusPendingExecution,
		model.TicketStatusFailed,
		model.TicketStatusStopped,
		model.TicketStatusInterrupted,
		model.TicketStatusRejected,
	}
	query, args, err := sqlx.In(
		`SELECT * FROM tickets WHERE status IN (?) ORDER BY updated_at DESC LIMIT ?`,
		statuses,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("build recent platform tickets query: %w", err)
	}
	query = r.db.Rebind(query)
	var tickets []model.Ticket
	if err := r.db.SelectContext(ctx, &tickets, query, args...); err != nil {
		return nil, fmt.Errorf("list recent platform tickets: %w", err)
	}
	return tickets, nil
}

func (r *TicketRepo) PlatformQueueStats(ctx context.Context, longPendingAfter time.Time) (*PlatformQueueStats, error) {
	stats := &PlatformQueueStats{}
	now := timeutil.NowUTC()
	err := r.db.GetContext(ctx, stats,
		`SELECT
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_review,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_execution,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS executing,
		 COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS needs_admin_attention,
		 COALESCE(SUM(CASE WHEN status IN (?, ?, ?) AND updated_at >= ? THEN 1 ELSE 0 END), 0) AS failed_today,
		 COALESCE(SUM(CASE WHEN status IN (?, ?, ?) AND updated_at >= ? THEN 1 ELSE 0 END), 0) AS failed_7d,
		 COALESCE(SUM(CASE WHEN status IN (?, ?) AND updated_at < ? THEN 1 ELSE 0 END), 0) AS long_pending
		 FROM tickets`,
		model.TicketStatusPendingReview,
		model.TicketStatusPendingExecution,
		model.TicketStatusExecuting,
		model.TicketStatusNeedsAdminAttention,
		model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted, now.Add(-24*time.Hour),
		model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted, now.Add(-7*24*time.Hour),
		model.TicketStatusPendingReview, model.TicketStatusPendingExecution, longPendingAfter,
	)
	if err != nil {
		return nil, fmt.Errorf("load platform queue stats: %w", err)
	}
	return stats, nil
}

func (r *TicketRepo) LongPendingTickets(ctx context.Context, longPendingAfter time.Time, limit int) ([]model.Ticket, error) {
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	statuses := []model.TicketStatus{model.TicketStatusPendingReview, model.TicketStatusPendingExecution}
	query, args, err := sqlx.In(
		`SELECT * FROM tickets WHERE status IN (?) AND updated_at < ? ORDER BY updated_at ASC LIMIT ?`,
		statuses,
		longPendingAfter,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("build long pending tickets query: %w", err)
	}
	query = r.db.Rebind(query)
	var tickets []model.Ticket
	if err := r.db.SelectContext(ctx, &tickets, query, args...); err != nil {
		return nil, fmt.Errorf("list long pending tickets: %w", err)
	}
	return tickets, nil
}

func (r *TicketRepo) PlatformAgingStats(ctx context.Context) (*PlatformAgingStats, error) {
	now := timeutil.NowUTC()
	var row struct {
		AvgReviewAge        sql.NullFloat64 `db:"avg_review_age"`
		AvgExecutionWaitAge sql.NullFloat64 `db:"avg_execution_wait_age"`
		MaxReviewAge        sql.NullInt64   `db:"max_review_age"`
		MaxExecutionWaitAge sql.NullInt64   `db:"max_execution_wait_age"`
	}
	if err := r.db.GetContext(ctx, &row,
		`SELECT
		 AVG(CASE WHEN status = ? THEN TIMESTAMPDIFF(MINUTE, created_at, ?) END) AS avg_review_age,
		 AVG(CASE WHEN status = ? THEN TIMESTAMPDIFF(MINUTE, updated_at, ?) END) AS avg_execution_wait_age,
		 MAX(CASE WHEN status = ? THEN TIMESTAMPDIFF(MINUTE, created_at, ?) END) AS max_review_age,
		 MAX(CASE WHEN status = ? THEN TIMESTAMPDIFF(MINUTE, updated_at, ?) END) AS max_execution_wait_age
		 FROM tickets`,
		model.TicketStatusPendingReview, now,
		model.TicketStatusPendingExecution, now,
		model.TicketStatusPendingReview, now,
		model.TicketStatusPendingExecution, now,
	); err != nil {
		return nil, fmt.Errorf("load platform aging stats: %w", err)
	}
	stats := &PlatformAgingStats{}
	if row.AvgReviewAge.Valid {
		stats.AvgReviewAgeMinutes = &row.AvgReviewAge.Float64
	}
	if row.AvgExecutionWaitAge.Valid {
		stats.AvgExecutionWaitAgeMinutes = &row.AvgExecutionWaitAge.Float64
	}
	if row.MaxReviewAge.Valid {
		stats.MaxReviewAgeMinutes = &row.MaxReviewAge.Int64
	}
	if row.MaxExecutionWaitAge.Valid {
		stats.MaxExecutionWaitAgeMinutes = &row.MaxExecutionWaitAge.Int64
	}
	var avgDuration sql.NullFloat64
	if err := r.db.GetContext(ctx, &avgDuration,
		`SELECT AVG(duration_ms) FROM ticket_executions WHERE status = 'completed' AND duration_ms IS NOT NULL AND completed_at >= ?`,
		now.Add(-7*24*time.Hour),
	); err != nil {
		return nil, fmt.Errorf("load platform execution duration stats: %w", err)
	}
	if avgDuration.Valid {
		stats.AvgExecutionDurationMs = &avgDuration.Float64
	}
	return stats, nil
}

func (r *TicketRepo) PlatformExecutionRiskStats(ctx context.Context) (*PlatformExecutionRiskStats, error) {
	since := timeutil.NowUTC().Add(-7 * 24 * time.Hour)
	stats := &PlatformExecutionRiskStats{}
	if err := r.db.GetContext(ctx, stats,
		`SELECT
		 COALESCE(SUM(CASE WHEN status IN ('failed', 'stopped') THEN 1 ELSE 0 END), 0) AS recent_failed,
		 COALESCE(SUM(CASE WHEN outcome_confidence = 'manually_stopped' THEN 1 ELSE 0 END), 0) AS manually_stopped,
		 COALESCE(SUM(CASE WHEN interruption_reason = 'service_shutdown' THEN 1 ELSE 0 END), 0) AS service_shutdown,
		 COALESCE(SUM(CASE WHEN outcome_confidence = 'outcome_unknown' THEN 1 ELSE 0 END), 0) AS outcome_unknown,
		 COALESCE(SUM(CASE WHEN outcome_confidence = 'not_sent' THEN 1 ELSE 0 END), 0) AS not_sent,
		 COALESCE(SUM(CASE WHEN outcome_confidence = 'failed' THEN 1 ELSE 0 END), 0) AS db_explicit_error
		 FROM ticket_executions
		 WHERE completed_at >= ?`,
		since,
	); err != nil {
		return nil, fmt.Errorf("load execution risk stats: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.ByOutcome,
		`SELECT COALESCE(NULLIF(outcome_confidence, ''), 'unknown') AS key_name, COUNT(*) AS count
		 FROM ticket_executions
		 WHERE completed_at >= ? AND status IN ('failed', 'stopped')
		 GROUP BY COALESCE(NULLIF(outcome_confidence, ''), 'unknown')
		 ORDER BY count DESC`,
		since,
	); err != nil {
		return nil, fmt.Errorf("count execution risk outcomes: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.ByInterruption,
		`SELECT COALESCE(NULLIF(interruption_reason, ''), 'db_explicit_error') AS key_name, COUNT(*) AS count
		 FROM ticket_executions
		 WHERE completed_at >= ? AND status IN ('failed', 'stopped')
		 GROUP BY COALESCE(NULLIF(interruption_reason, ''), 'db_explicit_error')
		 ORDER BY count DESC`,
		since,
	); err != nil {
		return nil, fmt.Errorf("count execution risk interruptions: %w", err)
	}
	return stats, nil
}

func (r *TicketRepo) RecentFailedExecutionTickets(ctx context.Context, limit int) ([]model.Ticket, error) {
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	statuses := []model.TicketStatus{model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted}
	query, args, err := sqlx.In(
		`SELECT * FROM tickets WHERE status IN (?) ORDER BY updated_at DESC LIMIT ?`,
		statuses,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("build failed execution tickets query: %w", err)
	}
	query = r.db.Rebind(query)
	var tickets []model.Ticket
	if err := r.db.SelectContext(ctx, &tickets, query, args...); err != nil {
		return nil, fmt.Errorf("list failed execution tickets: %w", err)
	}
	return tickets, nil
}

func (r *TicketRepo) PlatformTopUsageStats(ctx context.Context) (*PlatformTopUsageStats, error) {
	since := timeutil.NowUTC().Add(-30 * 24 * time.Hour)
	stats := &PlatformTopUsageStats{}
	if err := r.db.SelectContext(ctx, &stats.Submitters,
		`SELECT t.submitter_id AS user_id, u.username AS username, COUNT(*) AS count
		 FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id
		 WHERE t.created_at >= ?
		 GROUP BY t.submitter_id, u.username
		 ORDER BY count DESC LIMIT 8`,
		since,
	); err != nil {
		return nil, fmt.Errorf("count top submitters: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.DBConnectionsByTickets,
		`SELECT COALESCE(dc.name, CONCAT('#', t.db_connection_id)) AS key_name, COUNT(*) AS count
		 FROM tickets t LEFT JOIN db_connections dc ON dc.id = t.db_connection_id
		 WHERE t.created_at >= ? AND t.db_connection_id IS NOT NULL
		 GROUP BY t.db_connection_id, dc.name
		 ORDER BY count DESC LIMIT 8`,
		since,
	); err != nil {
		return nil, fmt.Errorf("count top ticket db connections: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.FailedDBConnections,
		`SELECT COALESCE(dc.name, CONCAT('#', t.db_connection_id)) AS key_name, COUNT(*) AS count
		 FROM tickets t LEFT JOIN db_connections dc ON dc.id = t.db_connection_id
		 WHERE t.updated_at >= ? AND t.db_connection_id IS NOT NULL AND t.status IN (?, ?, ?)
		 GROUP BY t.db_connection_id, dc.name
		 ORDER BY count DESC LIMIT 8`,
		since, model.TicketStatusFailed, model.TicketStatusStopped, model.TicketStatusInterrupted,
	); err != nil {
		return nil, fmt.Errorf("count top failed db connections: %w", err)
	}
	if err := r.db.SelectContext(ctx, &stats.SQLExportsByUser,
		`SELECT t.submitter_id AS user_id, u.username AS username, COUNT(*) AS count
		 FROM tickets t LEFT JOIN users u ON u.id = t.submitter_id
		 WHERE t.created_at >= ? AND t.ticket_type = ?
		 GROUP BY t.submitter_id, u.username
		 ORDER BY count DESC LIMIT 8`,
		since, model.TicketTypeSQLExport,
	); err != nil {
		return nil, fmt.Errorf("count top sql export users: %w", err)
	}
	return stats, nil
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

func (r *TicketRepo) EnsureExecutions(ctx context.Context, ticketID uint64, statements []model.TicketExecution) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ensure ticket executions tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_executions WHERE ticket_id = ? FOR UPDATE`, ticketID).Scan(&existing); err != nil {
		return fmt.Errorf("count ticket executions: %w", err)
	}
	if existing > 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit ensure ticket executions tx: %w", err)
		}
		tx = nil
		return nil
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ticket_executions (ticket_id, seq, sql_stmt, status, started_at) VALUES (?, ?, ?, 'pending', NULL)`,
			ticketID, statement.Seq, statement.SQLStmt,
		); err != nil {
			return fmt.Errorf("create pending ticket execution: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ensure ticket executions tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *TicketRepo) GetExecution(ctx context.Context, ticketID uint64, executionID uint64) (*model.TicketExecution, error) {
	var exec model.TicketExecution
	err := r.db.GetContext(ctx, &exec, `SELECT * FROM ticket_executions WHERE id = ? AND ticket_id = ?`, executionID, ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &exec, err
}

func (r *TicketRepo) MarkTicketExecuting(ctx context.Context, ticketID uint64, executorID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets
		 SET status = CASE WHEN status = 'pending_execution' THEN 'executing' ELSE status END,
		     executor_id = COALESCE(executor_id, ?),
		     started_at = COALESCE(started_at, ?),
		     updated_at = ?
		 WHERE id = ? AND status IN ('pending_execution', 'executing')`,
		executorID, timeutil.NowUTC(), timeutil.NowUTC(), ticketID,
	)
	return err
}

func (r *TicketRepo) SetExecutorIfEmpty(ctx context.Context, ticketID uint64, executorID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET executor_id = COALESCE(executor_id, ?), updated_at = ? WHERE id = ?`,
		executorID, timeutil.NowUTC(), ticketID,
	)
	return err
}

func (r *TicketRepo) SetExecutionAggregateStatus(ctx context.Context, ticketID uint64, status model.TicketStatus) error {
	now := timeutil.NowUTC()
	if status == model.TicketStatusCompleted || status == model.TicketStatusFailed || status == model.TicketStatusStopped {
		_, err := r.db.ExecContext(ctx,
			`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
			status, now, now, ticketID,
		)
		return err
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, ticketID,
	)
	return err
}

func (r *TicketRepo) MarkExecutionRunningIfPending(ctx context.Context, id uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = 'running',
		     started_at = ?,
		     completed_at = NULL,
		     error_msg = NULL,
		     duration_ms = NULL,
		     sent_to_db_at = NULL,
		     db_process_type = NULL,
		     db_process_id = NULL,
		     interruption_reason = NULL,
		     outcome_confidence = NULL
		 WHERE id = ? AND status = 'pending'`,
		timeutil.NowUTC(), id,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (r *TicketRepo) MarkExecutionRunning(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = 'running',
		     started_at = ?,
		     sent_to_db_at = NULL,
		     db_process_type = NULL,
		     db_process_id = NULL,
		     interruption_reason = NULL,
		     outcome_confidence = NULL
		 WHERE id = ?`,
		timeutil.NowUTC(), id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionSentToDB(ctx context.Context, id uint64, processType string, processID uint64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET sent_to_db_at = COALESCE(sent_to_db_at, ?),
		     db_process_type = ?,
		     db_process_id = ?,
		     outcome_confidence = ?
		 WHERE id = ? AND status = 'running'`,
		timeutil.NowUTC(), processType, processID, "sent_to_db", id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionStopped(ctx context.Context, id uint64, message string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = 'stopped',
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = NULL,
		     interruption_reason = ?,
		     outcome_confidence = ?
		 WHERE id = ?`,
		message, timeutil.NowUTC(), "manually_stopped", "manually_stopped", id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionDone(ctx context.Context, id uint64, rowsAffected *int64, durationMs *int64, errMsg *string) error {
	status := "completed"
	confidence := "completed"
	if errMsg != nil {
		status = "failed"
		confidence = "failed"
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = ?,
		     rows_affected = ?,
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = ?,
		     interruption_reason = NULL,
		     outcome_confidence = ?
		 WHERE id = ?`,
		status, rowsAffected, errMsg, timeutil.NowUTC(), durationMs, confidence, id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionFailedWithOutcome(ctx context.Context, id uint64, durationMs *int64, errMsg, interruptionReason, outcomeConfidence string) error {
	var reason any
	if strings.TrimSpace(interruptionReason) != "" {
		reason = interruptionReason
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = 'failed',
		     rows_affected = NULL,
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = ?,
		     interruption_reason = ?,
		     outcome_confidence = ?
		 WHERE id = ?`,
		errMsg, timeutil.NowUTC(), durationMs, reason, outcomeConfidence, id,
	)
	return err
}

func (r *TicketRepo) MarkExecutionInterrupted(ctx context.Context, id uint64, message, reason, outcomeConfidence string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE ticket_executions
		 SET status = 'failed',
		     rows_affected = NULL,
		     error_msg = ?,
		     completed_at = ?,
		     duration_ms = NULL,
		     interruption_reason = ?,
		     outcome_confidence = ?
		 WHERE id = ?`,
		message, timeutil.NowUTC(), reason, outcomeConfidence, id,
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
			 (ticket_id, seq, sql_stmt, phase, validation_stage, statement_kind, object_type, tables_json, validation_method, scan_rows, status, message, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ticketID,
			result.Seq,
			result.SQLStmt,
			result.Phase,
			result.ValidationStage,
			result.StatementKind,
			result.ObjectType,
			result.Tables,
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
		`SELECT id, ticket_id, seq, sql_stmt, phase, validation_stage, statement_kind, object_type, tables_json, validation_method, scan_rows, status, message, created_at
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

// RecoverExecutingTickets reconciles tickets left in executing state after a service restart.
func (r *TicketRepo) RecoverExecutingTickets(ctx context.Context) ([]TicketExecutionRecovery, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ticket recovery tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var ticketIDs []uint64
	if err := tx.SelectContext(ctx, &ticketIDs, `SELECT id FROM tickets WHERE status = ? FOR UPDATE`, model.TicketStatusExecuting); err != nil {
		return nil, fmt.Errorf("list executing tickets for recovery: %w", err)
	}

	recoveries := make([]TicketExecutionRecovery, 0, len(ticketIDs))
	now := timeutil.NowUTC()
	const restartReason = "service restarted during execution; database outcome unknown"
	for _, ticketID := range ticketIDs {
		var executions []model.TicketExecution
		if err := tx.SelectContext(ctx, &executions, `SELECT * FROM ticket_executions WHERE ticket_id = ? ORDER BY seq`, ticketID); err != nil {
			return nil, fmt.Errorf("list ticket executions for recovery: %w", err)
		}

		if len(executions) == 0 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
				model.TicketStatusFailed, now, now, ticketID,
			); err != nil {
				return nil, fmt.Errorf("mark legacy executing ticket failed: %w", err)
			}
			recoveries = append(recoveries, TicketExecutionRecovery{
				TicketID: ticketID,
				Status:   model.TicketStatusFailed,
				Reason:   "service restarted during legacy execution; execution progress unknown",
			})
			continue
		}

		failedExecutionIDs := make([]uint64, 0)
		failedExecutions := make([]TicketExecutionRecoveryDetail, 0)
		pending, running, completed, failed := 0, 0, 0, 0
		for _, execRow := range executions {
			switch execRow.Status {
			case "pending":
				pending++
			case "running":
				running++
				failed++
				failedExecutionIDs = append(failedExecutionIDs, execRow.ID)
				failedExecutions = append(failedExecutions, TicketExecutionRecoveryDetail{
					ExecutionID:        execRow.ID,
					Seq:                execRow.Seq,
					SentToDBAt:         execRow.SentToDBAt,
					DBProcessType:      execRow.DBProcessType,
					DBProcessID:        execRow.DBProcessID,
					InterruptionReason: "service_restart",
					OutcomeConfidence:  "outcome_unknown",
				})
			case "completed":
				completed++
			case "failed", "stopped":
				failed++
			}
		}

		if len(failedExecutionIDs) > 0 {
			for _, execRow := range executions {
				if execRow.Status != "running" {
					continue
				}
				message := ticketExecutionRestartMessage(execRow)
				if _, err := tx.ExecContext(ctx,
					`UPDATE ticket_executions
					 SET status = 'failed',
					     error_msg = ?,
					     completed_at = ?,
					     duration_ms = NULL,
					     interruption_reason = ?,
					     outcome_confidence = ?
					 WHERE id = ?`,
					message, now, "service_restart", "outcome_unknown", execRow.ID,
				); err != nil {
					return nil, fmt.Errorf("mark running ticket execution failed: %w", err)
				}
			}
			running = 0
		}

		status := aggregateTicketStatusFromCounts(len(executions), pending, running, completed, failed)
		if status == model.TicketStatusCompleted || status == model.TicketStatusFailed || status == model.TicketStatusStopped {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tickets SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
				status, now, now, ticketID,
			); err != nil {
				return nil, fmt.Errorf("mark recovered ticket terminal: %w", err)
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tickets SET status = ?, updated_at = ? WHERE id = ?`,
				status, now, ticketID,
			); err != nil {
				return nil, fmt.Errorf("mark recovered ticket resumable: %w", err)
			}
		}

		reason := "service restarted during execution; ticket execution state recovered"
		if len(failedExecutionIDs) > 0 {
			reason = restartReason
		}
		recoveries = append(recoveries, TicketExecutionRecovery{
			TicketID:           ticketID,
			Status:             status,
			Reason:             reason,
			FailedExecutionIDs: failedExecutionIDs,
			FailedExecutions:   failedExecutions,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ticket recovery tx: %w", err)
	}
	tx = nil
	return recoveries, nil
}

func ticketExecutionRestartMessage(execRow model.TicketExecution) string {
	if execRow.SentToDBAt == nil {
		return "service restarted while statement was running; database send state unknown"
	}
	if execRow.DBProcessType != nil && strings.TrimSpace(*execRow.DBProcessType) != "" && execRow.DBProcessID != nil && *execRow.DBProcessID != 0 {
		return fmt.Sprintf(
			"service restarted during execution; database outcome unknown; last known %s=%d",
			strings.TrimSpace(*execRow.DBProcessType),
			*execRow.DBProcessID,
		)
	}
	return "service restarted after statement was sent to database; database outcome unknown"
}

func aggregateTicketStatusFromCounts(total, pending, running, completed, failed int) model.TicketStatus {
	switch {
	case total == 0:
		return model.TicketStatusFailed
	case pending == total:
		return model.TicketStatusPendingExecution
	case running > 0:
		return model.TicketStatusExecuting
	case pending > 0 && completed+failed > 0:
		return model.TicketStatusExecuting
	case completed == total:
		return model.TicketStatusCompleted
	case completed+failed == total && failed > 0:
		return model.TicketStatusFailed
	default:
		return model.TicketStatusExecuting
	}
}
