package model

import "time"

type QueryAccessScopeMode string

const (
	QueryAccessScopeModeDatabase QueryAccessScopeMode = "database"
	QueryAccessScopeModeTable    QueryAccessScopeMode = "table"
)

type QueryAccessEffect string

const (
	QueryAccessEffectAllow QueryAccessEffect = "allow"
	QueryAccessEffectDeny  QueryAccessEffect = "deny"
)

type QueryAccessSubjectType string

const (
	QueryAccessSubjectTypeUser      QueryAccessSubjectType = "user"
	QueryAccessSubjectTypeAuthGroup QueryAccessSubjectType = "auth_group"
)

type QueryAccessTicketItem struct {
	ID               uint64               `db:"id"            json:"id"`
	TicketID         uint64               `db:"ticket_id"     json:"ticket_id"`
	ConnectionID     uint64               `db:"connection_id" json:"connection_id"`
	DBConnectionName *string              `db:"-"             json:"db_connection_name,omitempty"`
	ScopeMode        QueryAccessScopeMode `db:"scope_mode"    json:"scope_mode"`
	DatabaseName     string               `db:"database_name" json:"database_name"`
	TableName        *string              `db:"table_name"    json:"table_name,omitempty"`
	Effect           QueryAccessEffect    `db:"effect"        json:"effect"`
	DatabasePattern  string               `db:"database_pattern" json:"database_pattern"`
	TablePattern     string               `db:"table_pattern"    json:"table_pattern"`
	CreatedAt        time.Time            `db:"created_at"    json:"created_at"`
}

type QueryAccessGrant struct {
	ID             uint64     `db:"id"               json:"id"`
	SubjectType    string     `db:"subject_type"     json:"subject_type"`
	SubjectID      uint64     `db:"subject_id"       json:"subject_id"`
	ConnectionID   uint64     `db:"connection_id"    json:"connection_id"`
	DatabaseName   *string    `db:"database_name"    json:"database_name,omitempty"`
	TableName      *string    `db:"table_name"       json:"table_name,omitempty"`
	GrantedVia     string     `db:"granted_via"      json:"granted_via"`
	SourceTicketID *uint64    `db:"source_ticket_id" json:"source_ticket_id,omitempty"`
	ExpiresAt      *time.Time `db:"expires_at"       json:"expires_at,omitempty"`
	RevokedAt      *time.Time `db:"revoked_at"       json:"revoked_at,omitempty"`
	RevokedBy      *uint64    `db:"revoked_by"       json:"revoked_by,omitempty"`
	CreatedBy      *uint64    `db:"created_by"       json:"created_by,omitempty"`
	CreatedAt      time.Time  `db:"created_at"       json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at"       json:"updated_at"`
}

type QueryAccessRule struct {
	ID              uint64                 `db:"id"               json:"id"`
	SubjectType     QueryAccessSubjectType `db:"subject_type"     json:"subject_type"`
	SubjectID       uint64                 `db:"subject_id"       json:"subject_id"`
	Effect          QueryAccessEffect      `db:"effect"           json:"effect"`
	ConnectionID    uint64                 `db:"connection_id"    json:"connection_id"`
	DatabasePattern string                 `db:"database_pattern" json:"database_pattern"`
	TablePattern    string                 `db:"table_pattern"    json:"table_pattern"`
	GrantedVia      string                 `db:"granted_via"      json:"granted_via"`
	SourceTicketID  *uint64                `db:"source_ticket_id" json:"source_ticket_id,omitempty"`
	SourceTicketNo  *string                `db:"source_ticket_no" json:"source_ticket_no,omitempty"`
	ExpiresAt       *time.Time             `db:"expires_at"       json:"expires_at,omitempty"`
	RevokedAt       *time.Time             `db:"revoked_at"       json:"revoked_at,omitempty"`
	RevokedBy       *uint64                `db:"revoked_by"       json:"revoked_by,omitempty"`
	CreatedBy       *uint64                `db:"created_by"       json:"created_by,omitempty"`
	UpdatedBy       *uint64                `db:"updated_by"       json:"updated_by,omitempty"`
	CreatedAt       time.Time              `db:"created_at"       json:"created_at"`
	UpdatedAt       time.Time              `db:"updated_at"       json:"updated_at"`
}
