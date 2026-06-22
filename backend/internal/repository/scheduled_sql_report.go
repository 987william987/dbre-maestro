package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type ScheduledSQLReportRepo struct {
	db *sqlx.DB
}

func NewScheduledSQLReportRepo(db *sqlx.DB) *ScheduledSQLReportRepo {
	return &ScheduledSQLReportRepo{db: db}
}

type ScheduledSQLReportInput struct {
	Name             string
	Description      *string
	DBConnectionID   uint64
	DatabaseName     *string
	SchemaName       *string
	SQLContent       string
	CronExpression   string
	Timezone         string
	RecipientUserIDs []uint64
	IsActive         bool
	NextRunAt        *time.Time
	UserID           uint64
}

func (r *ScheduledSQLReportRepo) List(ctx context.Context) ([]model.ScheduledSQLReport, error) {
	var reports []model.ScheduledSQLReport
	if err := r.db.SelectContext(ctx, &reports, `SELECT * FROM scheduled_sql_reports ORDER BY created_at DESC, id DESC`); err != nil {
		return nil, err
	}
	return hydrateReportRecipients(reports)
}

func (r *ScheduledSQLReportRepo) GetByID(ctx context.Context, id uint64) (*model.ScheduledSQLReport, error) {
	var report model.ScheduledSQLReport
	err := r.db.GetContext(ctx, &report, `SELECT * FROM scheduled_sql_reports WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := hydrateReportRecipient(&report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (r *ScheduledSQLReportRepo) Create(ctx context.Context, input ScheduledSQLReportInput) (*model.ScheduledSQLReport, error) {
	recipients, err := json.Marshal(input.RecipientUserIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal report recipients: %w", err)
	}
	now := timeutil.NowUTC()
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO scheduled_sql_reports
		(name, description, db_connection_id, database_name, schema_name, sql_content, cron_expression, timezone, recipient_user_ids, is_active, next_run_at, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.Name, input.Description, input.DBConnectionID, input.DatabaseName, input.SchemaName, input.SQLContent,
		input.CronExpression, input.Timezone, string(recipients), input.IsActive, input.NextRunAt, input.UserID, input.UserID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create scheduled sql report: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *ScheduledSQLReportRepo) Update(ctx context.Context, id uint64, input ScheduledSQLReportInput) (*model.ScheduledSQLReport, error) {
	recipients, err := json.Marshal(input.RecipientUserIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal report recipients: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE scheduled_sql_reports
		SET name = ?, description = ?, db_connection_id = ?, database_name = ?, schema_name = ?, sql_content = ?,
		    cron_expression = ?, timezone = ?, recipient_user_ids = ?, is_active = ?, next_run_at = ?, updated_by = ?, updated_at = ?
		WHERE id = ?`,
		input.Name, input.Description, input.DBConnectionID, input.DatabaseName, input.SchemaName, input.SQLContent,
		input.CronExpression, input.Timezone, string(recipients), input.IsActive, input.NextRunAt, input.UserID, timeutil.NowUTC(), id,
	)
	if err != nil {
		return nil, fmt.Errorf("update scheduled sql report: %w", err)
	}
	return r.GetByID(ctx, id)
}

func (r *ScheduledSQLReportRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_sql_reports WHERE id = ?`, id)
	return err
}

func (r *ScheduledSQLReportRepo) GetDue(ctx context.Context, now time.Time, limit int) ([]model.ScheduledSQLReport, error) {
	var reports []model.ScheduledSQLReport
	if err := r.db.SelectContext(ctx, &reports, `
		SELECT * FROM scheduled_sql_reports
		WHERE is_active = 1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY next_run_at ASC, id ASC
		LIMIT ?`, now.UTC(), limit); err != nil {
		return nil, err
	}
	return hydrateReportRecipients(reports)
}

func (r *ScheduledSQLReportRepo) ClaimDue(ctx context.Context, reportID uint64, currentNextRunAt *time.Time, placeholderNextRunAt time.Time) (bool, error) {
	var res sql.Result
	var err error
	if currentNextRunAt == nil {
		res, err = r.db.ExecContext(ctx, `
			UPDATE scheduled_sql_reports
			SET next_run_at = ?, updated_at = ?
			WHERE id = ? AND is_active = 1 AND next_run_at IS NULL`,
			placeholderNextRunAt.UTC(), timeutil.NowUTC(), reportID)
	} else {
		res, err = r.db.ExecContext(ctx, `
			UPDATE scheduled_sql_reports
			SET next_run_at = ?, updated_at = ?
			WHERE id = ? AND is_active = 1 AND next_run_at = ?`,
			placeholderNextRunAt.UTC(), timeutil.NowUTC(), reportID, currentNextRunAt.UTC())
	}
	if err != nil {
		return false, err
	}
	rows, _ := res.RowsAffected()
	return rows > 0, nil
}

func (r *ScheduledSQLReportRepo) CreateRun(ctx context.Context, reportID uint64, startedAt time.Time) (uint64, error) {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO scheduled_sql_report_runs (report_id, status, started_at, created_at)
		VALUES (?, 'running', ?, ?)`, reportID, startedAt.UTC(), timeutil.NowUTC())
	if err != nil {
		return 0, fmt.Errorf("create scheduled sql report run: %w", err)
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (r *ScheduledSQLReportRepo) FinishRun(ctx context.Context, runID uint64, status string, rowCount int, fileName *string, errMessage *string, finishedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_sql_report_runs
		SET status = ?, row_count = ?, file_name = ?, error_message = ?, finished_at = ?
		WHERE id = ?`, status, rowCount, fileName, errMessage, finishedAt.UTC(), runID)
	return err
}

func (r *ScheduledSQLReportRepo) ListRuns(ctx context.Context, reportID uint64, limit int) ([]model.ScheduledSQLReportRun, error) {
	var runs []model.ScheduledSQLReportRun
	if err := r.db.SelectContext(ctx, &runs, `
		SELECT * FROM scheduled_sql_report_runs
		WHERE report_id = ?
		ORDER BY started_at DESC, id DESC
		LIMIT ?`, reportID, limit); err != nil {
		return nil, err
	}
	return runs, nil
}

func (r *ScheduledSQLReportRepo) UpdateRunState(ctx context.Context, reportID uint64, lastRunAt time.Time, status string, errMessage *string, nextRunAt *time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_sql_reports
		SET last_run_at = ?, last_status = ?, last_error = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?`, lastRunAt.UTC(), status, errMessage, nextRunAt, timeutil.NowUTC(), reportID)
	return err
}

func hydrateReportRecipients(reports []model.ScheduledSQLReport) ([]model.ScheduledSQLReport, error) {
	for i := range reports {
		if err := hydrateReportRecipient(&reports[i]); err != nil {
			return nil, err
		}
	}
	return reports, nil
}

func hydrateReportRecipient(report *model.ScheduledSQLReport) error {
	if report == nil {
		return nil
	}
	if report.RecipientsJSON == "" {
		report.RecipientUserIDs = []uint64{}
		return nil
	}
	if err := json.Unmarshal([]byte(report.RecipientsJSON), &report.RecipientUserIDs); err != nil {
		return fmt.Errorf("decode scheduled sql report recipients: %w", err)
	}
	return nil
}
