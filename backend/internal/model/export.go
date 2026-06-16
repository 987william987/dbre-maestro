package model

import "time"

type ExportStatus string

const (
	ExportStatusPending  ExportStatus = "pending"
	ExportStatusApproved ExportStatus = "approved"
	ExportStatusRejected ExportStatus = "rejected"
	ExportStatusReady    ExportStatus = "ready"
	ExportStatusExpired  ExportStatus = "expired"
)

type ExportRequest struct {
	ID              uint64       `db:"id"               json:"id"`
	TicketID        *uint64      `db:"ticket_id"        json:"ticket_id,omitempty"`
	RequesterID     uint64       `db:"requester_id"     json:"requester_id"`
	ApproverID      *uint64      `db:"approver_id"      json:"approver_id,omitempty"`
	DownloadToken   string       `db:"download_token"   json:"-"` // never expose in API
	SQLContent      string       `db:"sql_content"      json:"sql_content"`
	DBConnectionID  uint64       `db:"db_connection_id" json:"db_connection_id"`
	RowCount        *uint32      `db:"row_count"        json:"row_count,omitempty"`
	FilePath        *string      `db:"file_path"        json:"-"`
	Status          ExportStatus `db:"status"           json:"status"`
	ExpiresAt       time.Time    `db:"expires_at"       json:"expires_at"`
	DownloadedAt    *time.Time   `db:"downloaded_at"    json:"downloaded_at,omitempty"`
	CreatedAt       time.Time    `db:"created_at"       json:"created_at"`
}
