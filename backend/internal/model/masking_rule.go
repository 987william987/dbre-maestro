package model

import "time"

type MaskingRule struct {
	ID               uint64    `db:"id" json:"id"`
	DBConnectionID   *uint64   `db:"db_connection_id" json:"db_connection_id,omitempty"`
	TableName        string    `db:"table_name" json:"table_name"`
	ColumnName       string    `db:"column_name" json:"column_name"`
	MaskMode         string    `db:"mask_mode" json:"mask_mode"`
	CreatedBy        uint64    `db:"created_by" json:"created_by"`
	CreatedAt        time.Time `db:"created_at" json:"created_at"`
}
