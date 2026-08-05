package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type QueryAccessRepo struct {
	db *sqlx.DB
}

type AccessGovernanceStats struct {
	ExpiringSoon        int64 `db:"expiring_soon" json:"expiring_soon"`
	LongLived           int64 `db:"long_lived" json:"long_lived"`
	NeverExpires        int64 `db:"never_expires" json:"never_expires"`
	RecentlyRevoked     int64 `db:"recently_revoked" json:"recently_revoked"`
	SensitiveRequests7d int64 `db:"sensitive_requests_7d" json:"sensitive_requests_7d"`
	SQLExportRequests7d int64 `db:"sql_export_requests_7d" json:"sql_export_requests_7d"`
	ActiveRules         int64 `db:"active_rules" json:"active_rules"`
}

func NewQueryAccessRepo(db *sqlx.DB) *QueryAccessRepo {
	return &QueryAccessRepo{db: db}
}

func (r *QueryAccessRepo) CreateTicketItems(ctx context.Context, ticketID uint64, items []model.QueryAccessTicketItem) error {
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		rule := normalizeQueryAccessItem(item)
		if rule.DatabasePattern == "" {
			return fmt.Errorf("database_pattern is required")
		}
		if rule.TablePattern == "" {
			return fmt.Errorf("table_pattern is required")
		}
		if _, err := r.db.ExecContext(ctx,
			`INSERT INTO query_access_ticket_rules (ticket_id, effect, connection_id, database_pattern, table_pattern, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			ticketID,
			rule.Effect,
			rule.ConnectionID,
			rule.DatabasePattern,
			rule.TablePattern,
			timeutil.NowUTC(),
		); err != nil {
			return fmt.Errorf("create query access ticket rule: %w", err)
		}
	}
	return nil
}

func (r *QueryAccessRepo) ListTicketItems(ctx context.Context, ticketID uint64) ([]model.QueryAccessTicketItem, error) {
	items := make([]model.QueryAccessTicketItem, 0)
	if err := r.db.SelectContext(ctx, &items, `
		SELECT id, ticket_id, connection_id,
		       CASE WHEN table_pattern = '*' THEN 'database' ELSE 'table' END AS scope_mode,
		       database_pattern AS database_name,
		       NULLIF(table_pattern, '*') AS table_name,
		       effect, database_pattern, table_pattern, created_at
		FROM query_access_ticket_rules
		WHERE ticket_id = ?
		ORDER BY id ASC
	`, ticketID); err != nil {
		return nil, fmt.Errorf("list query access ticket rules: %w", err)
	}
	if len(items) > 0 {
		return items, nil
	}

	if err := r.db.SelectContext(ctx, &items,
		`SELECT id, ticket_id, connection_id, scope_mode, database_name, table_name,
		        'allow' AS effect,
		        database_name AS database_pattern,
		        COALESCE(NULLIF(table_name, ''), '*') AS table_pattern,
		        created_at
		 FROM query_access_ticket_items
		 WHERE ticket_id = ?
		 ORDER BY id ASC`,
		ticketID,
	); err != nil {
		return nil, fmt.Errorf("list legacy query access ticket items: %w", err)
	}
	return items, nil
}

func (r *QueryAccessRepo) CreateGrantsForTicket(ctx context.Context, ticketID, subjectID, actorID uint64, expiresAt *time.Time) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create query access grants tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := r.createGrantsForTicketTx(ctx, tx, ticketID, subjectID, actorID, expiresAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create query access grants tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *QueryAccessRepo) ApproveTicket(ctx context.Context, ticketID uint64, fromStatus model.TicketStatus, reviewerID uint64, comment *string, subjectID uint64, expiresAt *time.Time) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin approve query access tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	query := `UPDATE tickets SET status = ?, updated_at = ?, reviewer_id = ?`
	args := []any{model.TicketStatusApproved, timeutil.NowUTC(), reviewerID}
	if comment != nil {
		query += `, review_comment = ?`
		args = append(args, *comment)
	}
	if expiresAt != nil {
		query += `, approved_until = ?`
		args = append(args, expiresAt.UTC())
	}
	query += ` WHERE id = ? AND status = ? AND ticket_type = ?`
	args = append(args, ticketID, fromStatus, model.TicketTypeQueryAccess)

	res, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("approve query access ticket: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return false, nil
	}

	if err := r.createGrantsForTicketTx(ctx, tx, ticketID, subjectID, reviewerID, expiresAt); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit approve query access tx: %w", err)
	}
	tx = nil
	return true, nil
}

func (r *QueryAccessRepo) createGrantsForTicketTx(ctx context.Context, tx *sqlx.Tx, ticketID, subjectID, actorID uint64, expiresAt *time.Time) error {
	items, err := r.ListTicketItems(ctx, ticketID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("query access ticket has no items")
	}

	now := timeutil.NowUTC()
	for _, item := range items {
		var sourceTicketID *uint64
		sourceTicketID = &ticketID
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO query_access_rules
			 (subject_type, subject_id, effect, connection_id, database_pattern, table_pattern, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, 'ticket', ?, ?, NULL, NULL, ?, ?, ?, ?)`,
			"user",
			subjectID,
			normalizeQueryAccessEffect(item.Effect),
			item.ConnectionID,
			normalizePattern(firstNonEmpty(item.DatabasePattern, item.DatabaseName)),
			normalizePattern(firstNonEmpty(item.TablePattern, nullableString(item.TableName))),
			sourceTicketID,
			expiresAt,
			actorID,
			actorID,
			now,
			now,
		); err != nil {
			return fmt.Errorf("create query access rule: %w", err)
		}
	}
	return nil
}

func (r *QueryAccessRepo) RevokeGrantsByTicket(ctx context.Context, ticketID, actorID uint64) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin revoke query access grants tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	ok, err := r.revokeGrantsByTicketTx(ctx, tx, ticketID, actorID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit revoke query access grants tx: %w", err)
	}
	tx = nil
	return ok, nil
}

func (r *QueryAccessRepo) RevokeTicket(ctx context.Context, ticketID uint64, actorID uint64) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin revoke query access ticket tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	ok, err := r.revokeGrantsByTicketTx(ctx, tx, ticketID, actorID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tickets
		 SET status = ?, revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE id = ? AND ticket_type = ? AND status = ? AND revoked_at IS NULL`,
		model.TicketStatusStopped, timeutil.NowUTC(), actorID, timeutil.NowUTC(), ticketID, model.TicketTypeQueryAccess, model.TicketStatusApproved,
	)
	if err != nil {
		return false, fmt.Errorf("update query access ticket stopped: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit revoke query access ticket tx: %w", err)
	}
	tx = nil
	return true, nil
}

func (r *QueryAccessRepo) revokeGrantsByTicketTx(ctx context.Context, tx *sqlx.Tx, ticketID, actorID uint64) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE query_access_rules
		 SET revoked_at = ?, revoked_by = ?, updated_at = ?
		 WHERE source_ticket_id = ? AND revoked_at IS NULL`,
		timeutil.NowUTC(), actorID, timeutil.NowUTC(), ticketID,
	)
	if err != nil {
		return false, fmt.Errorf("revoke query access grants: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *QueryAccessRepo) AccessGovernanceStats(ctx context.Context) (*AccessGovernanceStats, error) {
	now := timeutil.NowUTC()
	stats := &AccessGovernanceStats{}
	if err := r.db.GetContext(ctx, stats,
		`SELECT
		 COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at > ? AND expires_at <= ? THEN 1 ELSE 0 END), 0) AS expiring_soon,
		 COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at IS NOT NULL AND expires_at > ? THEN 1 ELSE 0 END), 0) AS long_lived,
		 COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at IS NULL THEN 1 ELSE 0 END), 0) AS never_expires,
		 COALESCE(SUM(CASE WHEN revoked_at IS NOT NULL AND revoked_at >= ? THEN 1 ELSE 0 END), 0) AS recently_revoked,
		 COALESCE(SUM(CASE WHEN revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?) THEN 1 ELSE 0 END), 0) AS active_rules
		 FROM query_access_rules`,
		now, now.Add(7*24*time.Hour),
		now.Add(90*24*time.Hour),
		now.Add(-7*24*time.Hour),
		now,
	); err != nil {
		return nil, fmt.Errorf("load query access governance stats: %w", err)
	}
	if err := r.db.GetContext(ctx, &stats.SensitiveRequests7d,
		`SELECT COUNT(*) FROM tickets WHERE ticket_type = ? AND created_at >= ?`,
		model.TicketTypeSensitiveQueryAccess, now.Add(-7*24*time.Hour),
	); err != nil {
		return nil, fmt.Errorf("count sensitive access requests: %w", err)
	}
	if err := r.db.GetContext(ctx, &stats.SQLExportRequests7d,
		`SELECT COUNT(*) FROM tickets WHERE ticket_type = ? AND created_at >= ?`,
		model.TicketTypeSQLExport, now.Add(-7*24*time.Hour),
	); err != nil {
		return nil, fmt.Errorf("count sql export requests: %w", err)
	}
	return stats, nil
}

func (r *QueryAccessRepo) ListActiveRules(ctx context.Context, subjectID uint64, authGroupIDs []uint64, connectionID uint64) ([]model.QueryAccessRule, error) {
	args := []any{subjectID}
	authGroupClause := ""
	if len(authGroupIDs) > 0 {
		query, inArgs, err := sqlx.In(` OR (subject_type = 'auth_group' AND subject_id IN (?))`, authGroupIDs)
		if err != nil {
			return nil, err
		}
		authGroupClause = query
		args = append(args, inArgs...)
	}
	args = append(args, connectionID, timeutil.NowUTC())

	query := `
		SELECT id, subject_type, subject_id, effect, connection_id, database_pattern, table_pattern,
		       granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at
		FROM query_access_rules
		WHERE (subject_type = 'user' AND subject_id = ?` + authGroupClause + `)
		  AND connection_id = ?
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > ?)
		ORDER BY
		  CASE effect WHEN 'deny' THEN 0 ELSE 1 END,
		  id ASC
	`
	var rules []model.QueryAccessRule
	if err := r.db.SelectContext(ctx, &rules, r.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("list active query access rules: %w", err)
	}
	return rules, nil
}

func (r *QueryAccessRepo) ListActiveRulesForSubjects(ctx context.Context, subjectID uint64, authGroupIDs []uint64, limit int) ([]model.QueryAccessRule, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	args := []any{subjectID}
	authGroupClause := ""
	if len(authGroupIDs) > 0 {
		query, inArgs, err := sqlx.In(` OR (qar.subject_type = 'auth_group' AND qar.subject_id IN (?))`, authGroupIDs)
		if err != nil {
			return nil, err
		}
		authGroupClause = query
		args = append(args, inArgs...)
	}
	args = append(args, timeutil.NowUTC(), limit)

	query := `
		SELECT qar.id, qar.subject_type, qar.subject_id, qar.effect, qar.connection_id, qar.database_pattern, qar.table_pattern,
		       qar.granted_via, qar.source_ticket_id, t.ticket_no AS source_ticket_no,
		       qar.expires_at, qar.revoked_at, qar.revoked_by, qar.created_by, qar.updated_by, qar.created_at, qar.updated_at
		FROM query_access_rules qar
		LEFT JOIN tickets t ON t.id = qar.source_ticket_id
		WHERE (qar.subject_type = 'user' AND qar.subject_id = ?` + authGroupClause + `)
		  AND qar.revoked_at IS NULL
		  AND (qar.expires_at IS NULL OR qar.expires_at > ?)
		ORDER BY
		  CASE WHEN qar.expires_at IS NULL THEN 1 ELSE 0 END,
		  qar.expires_at ASC,
		  qar.id DESC
		LIMIT ?
	`
	rules := []model.QueryAccessRule{}
	if err := r.db.SelectContext(ctx, &rules, r.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("list active query access rules for subjects: %w", err)
	}
	return rules, nil
}

func (r *QueryAccessRepo) ListRules(ctx context.Context) ([]model.QueryAccessRule, error) {
	rules := make([]model.QueryAccessRule, 0)
	if err := r.db.SelectContext(ctx, &rules, `
		SELECT qar.id, qar.subject_type, qar.subject_id, qar.effect, qar.connection_id, qar.database_pattern, qar.table_pattern,
		       qar.granted_via, qar.source_ticket_id, t.ticket_no AS source_ticket_no,
		       qar.expires_at, qar.revoked_at, qar.revoked_by, qar.created_by, qar.updated_by, qar.created_at, qar.updated_at
		FROM query_access_rules qar
		LEFT JOIN tickets t ON t.id = qar.source_ticket_id
		ORDER BY
		  CASE WHEN qar.revoked_at IS NULL AND (qar.expires_at IS NULL OR qar.expires_at > ?) THEN 0 ELSE 1 END,
		  qar.id DESC
	`, timeutil.NowUTC()); err != nil {
		return nil, fmt.Errorf("list query access rules: %w", err)
	}
	return rules, nil
}

func (r *QueryAccessRepo) GetRule(ctx context.Context, ruleID uint64) (*model.QueryAccessRule, error) {
	var rule model.QueryAccessRule
	if err := r.db.GetContext(ctx, &rule, `
		SELECT id, subject_type, subject_id, effect, connection_id, database_pattern, table_pattern,
		       granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at
		FROM query_access_rules
		WHERE id = ?
	`, ruleID); err != nil {
		return nil, fmt.Errorf("get query access rule: %w", err)
	}
	return &rule, nil
}

func (r *QueryAccessRepo) CreateManualRule(ctx context.Context, rule model.QueryAccessRule, actorID uint64) (*model.QueryAccessRule, error) {
	rule.Effect = normalizeQueryAccessEffect(rule.Effect)
	rule.DatabasePattern = normalizePattern(rule.DatabasePattern)
	rule.TablePattern = normalizePattern(rule.TablePattern)
	if rule.SubjectType != model.QueryAccessSubjectTypeUser && rule.SubjectType != model.QueryAccessSubjectTypeAuthGroup {
		return nil, fmt.Errorf("invalid query access subject_type")
	}
	if rule.SubjectID == 0 || rule.ConnectionID == 0 {
		return nil, fmt.Errorf("subject_id and connection_id are required")
	}
	now := timeutil.NowUTC()
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO query_access_rules
		 (subject_type, subject_id, effect, connection_id, database_pattern, table_pattern, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'manual', NULL, ?, NULL, NULL, ?, ?, ?, ?)
	`, rule.SubjectType, rule.SubjectID, rule.Effect, rule.ConnectionID, rule.DatabasePattern, rule.TablePattern, rule.ExpiresAt, actorID, actorID, now, now)
	if err != nil {
		return nil, fmt.Errorf("create manual query access rule: %w", err)
	}
	id, _ := res.LastInsertId()
	rule.ID = uint64(id)
	rule.GrantedVia = "manual"
	rule.CreatedBy = &actorID
	rule.UpdatedBy = &actorID
	rule.CreatedAt = now
	rule.UpdatedAt = now
	return &rule, nil
}

func (r *QueryAccessRepo) ReplaceManualRule(ctx context.Context, oldRuleID uint64, rule model.QueryAccessRule, actorID uint64) (*model.QueryAccessRule, error) {
	rule.Effect = normalizeQueryAccessEffect(rule.Effect)
	rule.DatabasePattern = normalizePattern(rule.DatabasePattern)
	rule.TablePattern = normalizePattern(rule.TablePattern)
	if rule.SubjectType != model.QueryAccessSubjectTypeUser && rule.SubjectType != model.QueryAccessSubjectTypeAuthGroup {
		return nil, fmt.Errorf("invalid query access subject_type")
	}
	if rule.SubjectID == 0 || rule.ConnectionID == 0 {
		return nil, fmt.Errorf("subject_id and connection_id are required")
	}
	now := timeutil.NowUTC()
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin replace query access rule tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE query_access_rules
		SET revoked_at = ?, revoked_by = ?, updated_by = ?, updated_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, now, actorID, actorID, now, oldRuleID)
	if err != nil {
		return nil, fmt.Errorf("revoke old query access rule: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("query access rule not found or already revoked")
	}

	insertRes, err := tx.ExecContext(ctx, `
		INSERT INTO query_access_rules
		 (subject_type, subject_id, effect, connection_id, database_pattern, table_pattern, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, updated_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'manual', NULL, ?, NULL, NULL, ?, ?, ?, ?)
	`, rule.SubjectType, rule.SubjectID, rule.Effect, rule.ConnectionID, rule.DatabasePattern, rule.TablePattern, rule.ExpiresAt, actorID, actorID, now, now)
	if err != nil {
		return nil, fmt.Errorf("create replacement query access rule: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit replace query access rule tx: %w", err)
	}
	id, _ := insertRes.LastInsertId()
	rule.ID = uint64(id)
	rule.GrantedVia = "manual"
	rule.CreatedBy = &actorID
	rule.UpdatedBy = &actorID
	rule.CreatedAt = now
	rule.UpdatedAt = now
	return &rule, nil
}

func (r *QueryAccessRepo) RevokeRule(ctx context.Context, ruleID, actorID uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE query_access_rules
		SET revoked_at = ?, revoked_by = ?, updated_by = ?, updated_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, timeutil.NowUTC(), actorID, actorID, timeutil.NowUTC(), ruleID)
	if err != nil {
		return false, fmt.Errorf("revoke query access rule: %w", err)
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *QueryAccessRepo) ListActiveGrants(ctx context.Context, subjectID, connectionID uint64) ([]model.QueryAccessGrant, error) {
	grants := make([]model.QueryAccessGrant, 0)
	if err := r.db.SelectContext(ctx, &grants,
		`SELECT id, subject_type, subject_id, connection_id, database_name, table_name, granted_via, source_ticket_id, expires_at, revoked_at, revoked_by, created_by, created_at, updated_at
		 FROM query_access_grants
		 WHERE subject_type = 'user'
		   AND subject_id = ?
		   AND connection_id = ?
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > ?)
		 ORDER BY id ASC`,
		subjectID,
		connectionID,
		timeutil.NowUTC(),
	); err != nil {
		return nil, fmt.Errorf("list active query access grants: %w", err)
	}
	return grants, nil
}

func normalizeQueryAccessItem(item model.QueryAccessTicketItem) model.QueryAccessTicketItem {
	item.Effect = normalizeQueryAccessEffect(item.Effect)
	item.DatabasePattern = normalizePattern(firstNonEmpty(item.DatabasePattern, item.DatabaseName))
	item.TablePattern = normalizePattern(firstNonEmpty(item.TablePattern, nullableString(item.TableName)))
	if item.TablePattern == "" {
		item.TablePattern = "*"
	}
	if item.TablePattern == "*" {
		item.ScopeMode = model.QueryAccessScopeModeDatabase
		item.TableName = nil
	} else {
		item.ScopeMode = model.QueryAccessScopeModeTable
		table := item.TablePattern
		item.TableName = &table
	}
	item.DatabaseName = item.DatabasePattern
	return item
}

func normalizeQueryAccessEffect(effect model.QueryAccessEffect) model.QueryAccessEffect {
	switch effect {
	case model.QueryAccessEffectDeny:
		return model.QueryAccessEffectDeny
	default:
		return model.QueryAccessEffectAllow
	}
}

func normalizePattern(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "ALL" || trimmed == "all" {
		return "*"
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func trimmedNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
