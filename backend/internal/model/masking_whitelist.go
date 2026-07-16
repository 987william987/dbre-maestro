package model

import "time"

type MaskingWhitelist struct {
	ID             uint64    `db:"id" json:"id"`
	DBConnectionID uint64    `db:"db_connection_id" json:"db_connection_id"`
	DatabaseName   string    `db:"database_name" json:"database_name"`
	SchemaName     string    `db:"schema_name" json:"schema_name"`
	TableName      string    `db:"table_name" json:"table_name"`
	ColumnName     string    `db:"column_name" json:"column_name"`
	Enabled        bool      `db:"enabled" json:"enabled"`
	CreatedBy      uint64    `db:"created_by" json:"created_by"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
}
