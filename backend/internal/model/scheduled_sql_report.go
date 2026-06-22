package model

import "time"

type ScheduledSQLReport struct {
	ID               uint64     `db:"id"                 json:"id"`
	Name             string     `db:"name"               json:"name"`
	Description      *string    `db:"description"        json:"description,omitempty"`
	DBConnectionID   uint64     `db:"db_connection_id"   json:"db_connection_id"`
	DatabaseName     *string    `db:"database_name"      json:"database_name,omitempty"`
	SchemaName       *string    `db:"schema_name"        json:"schema_name,omitempty"`
	SQLContent       string     `db:"sql_content"        json:"sql_content"`
	CronExpression   string     `db:"cron_expression"    json:"cron_expression"`
	Timezone         string     `db:"timezone"           json:"timezone"`
	RecipientUserIDs []uint64   `db:"-"                  json:"recipient_user_ids"`
	RecipientsJSON   string     `db:"recipient_user_ids" json:"-"`
	IsActive         bool       `db:"is_active"          json:"is_active"`
	NextRunAt        *time.Time `db:"next_run_at"        json:"next_run_at,omitempty"`
	LastRunAt        *time.Time `db:"last_run_at"        json:"last_run_at,omitempty"`
	LastStatus       *string    `db:"last_status"        json:"last_status,omitempty"`
	LastError        *string    `db:"last_error"         json:"last_error,omitempty"`
	CreatedBy        uint64     `db:"created_by"         json:"created_by"`
	UpdatedBy        uint64     `db:"updated_by"         json:"updated_by"`
	CreatedAt        time.Time  `db:"created_at"         json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"         json:"updated_at"`
}

type ScheduledSQLReportRun struct {
	ID           uint64     `db:"id"            json:"id"`
	ReportID     uint64     `db:"report_id"     json:"report_id"`
	Status       string     `db:"status"        json:"status"`
	RowCount     int        `db:"row_count"     json:"row_count"`
	FileName     *string    `db:"file_name"     json:"file_name,omitempty"`
	ErrorMessage *string    `db:"error_message" json:"error_message,omitempty"`
	StartedAt    time.Time  `db:"started_at"    json:"started_at"`
	FinishedAt   *time.Time `db:"finished_at"   json:"finished_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
}
