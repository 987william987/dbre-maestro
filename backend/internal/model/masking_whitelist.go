package model

import "time"

type MaskingWhitelist struct {
	ID             uint64    `db:"id"               json:"id"`
	DBConnectionID *uint64   `db:"db_connection_id" json:"db_connection_id,omitempty"`
	TableName      string    `db:"table_name"       json:"table_name"`
	ColumnName     string    `db:"column_name"      json:"column_name"`
	UserID         *uint64   `db:"user_id"          json:"user_id,omitempty"`
	AuthGroupID    *uint64   `db:"auth_group_id"    json:"auth_group_id,omitempty"`
	AuthGroup      *string   `db:"auth_group"       json:"auth_group,omitempty"`
	CreatedBy      uint64    `db:"created_by"       json:"created_by"`
	CreatedAt      time.Time `db:"created_at"       json:"created_at"`
}
