package model

import "time"

type QueryHistoryEntry struct {
	ID               uint64    `db:"id"                 json:"id"`
	UserID           uint64    `db:"user_id"            json:"user_id"`
	DBConnectionID   uint64    `db:"db_connection_id"   json:"db_connection_id"`
	DBConnectionName string    `db:"db_connection_name" json:"db_connection_name"`
	DatabaseName     *string   `db:"database_name"      json:"database_name,omitempty"`
	SchemaName       *string   `db:"schema_name"        json:"schema_name,omitempty"`
	RedisDBIndex     *int      `db:"redis_db_index"     json:"redis_db_index,omitempty"`
	SQLContent       string    `db:"sql_content"        json:"sql_content"`
	RowCount         *int      `db:"row_count"          json:"row_count,omitempty"`
	DurationMs       int64     `db:"duration_ms"        json:"duration_ms"`
	CreatedAt        time.Time `db:"created_at"         json:"created_at"`
}

type SavedQuery struct {
	ID               uint64    `db:"id"                 json:"id"`
	UserID           uint64    `db:"user_id"            json:"user_id"`
	Label            string    `db:"label"              json:"label"`
	DBConnectionID   uint64    `db:"db_connection_id"   json:"db_connection_id"`
	DBConnectionName string    `db:"db_connection_name" json:"db_connection_name"`
	DatabaseName     *string   `db:"database_name"      json:"database_name,omitempty"`
	SchemaName       *string   `db:"schema_name"        json:"schema_name,omitempty"`
	RedisDBIndex     *int      `db:"redis_db_index"     json:"redis_db_index,omitempty"`
	SQLContent       string    `db:"sql_content"        json:"sql_content"`
	CreatedAt        time.Time `db:"created_at"         json:"created_at"`
	UpdatedAt        time.Time `db:"updated_at"         json:"updated_at"`
}
