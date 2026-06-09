package model

import "time"

type TicketStatus string

const (
	TicketStatusPendingReview    TicketStatus = "pending_review"
	TicketStatusApproved         TicketStatus = "approved"
	TicketStatusRejected         TicketStatus = "rejected"
	TicketStatusPendingExecution TicketStatus = "pending_execution"
	TicketStatusExecuting        TicketStatus = "executing"
	TicketStatusCompleted        TicketStatus = "completed"
	TicketStatusFailed           TicketStatus = "failed"
	TicketStatusStopped          TicketStatus = "stopped"
	TicketStatusInterrupted      TicketStatus = "interrupted"
)

type TicketType string

const (
	TicketTypeDDL TicketType = "ddl"
	TicketTypeDML TicketType = "dml"
)

type Ticket struct {
	ID             uint64       `db:"id"               json:"id"`
	TicketNo       string       `db:"ticket_no"        json:"ticket_no"`
	Title          string       `db:"title"            json:"title"`
	Description    *string      `db:"description"      json:"description,omitempty"`
	SQLContent     string       `db:"sql_content"      json:"sql_content"`
	TicketType     TicketType   `db:"ticket_type"      json:"ticket_type"`
	DBConnectionID *uint64      `db:"db_connection_id" json:"db_connection_id,omitempty"`
	Status         TicketStatus `db:"status"           json:"status"`
	SubmitterID    uint64       `db:"submitter_id"     json:"submitter_id"`
	ReviewerID     *uint64      `db:"reviewer_id"      json:"reviewer_id,omitempty"`
	ExecutorID     *uint64      `db:"executor_id"      json:"executor_id,omitempty"`
	ReviewComment  *string      `db:"review_comment"   json:"review_comment,omitempty"`
	RejectionReason *string     `db:"rejection_reason" json:"rejection_reason,omitempty"`
	ScheduledAt    *time.Time   `db:"scheduled_at"     json:"scheduled_at,omitempty"`
	StartedAt      *time.Time   `db:"started_at"       json:"started_at,omitempty"`
	CompletedAt    *time.Time   `db:"completed_at"     json:"completed_at,omitempty"`
	CreatedAt      time.Time    `db:"created_at"       json:"created_at"`
	UpdatedAt      time.Time    `db:"updated_at"       json:"updated_at"`
}

type TicketExecution struct {
	ID           uint64     `db:"id"`
	TicketID     uint64     `db:"ticket_id"`
	Seq          int        `db:"seq"`
	SQLStmt      string     `db:"sql_stmt"`
	Status       string     `db:"status"`
	RowsAffected *int64     `db:"rows_affected"`
	ErrorMsg     *string    `db:"error_msg"`
	StartedAt    *time.Time `db:"started_at"`
	CompletedAt  *time.Time `db:"completed_at"`
}
