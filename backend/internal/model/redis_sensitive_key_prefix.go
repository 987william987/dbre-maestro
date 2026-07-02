package model

import "time"

type RedisSensitiveKeyPrefix struct {
	ID             uint64    `db:"id" json:"id"`
	DBConnectionID uint64    `db:"db_connection_id" json:"db_connection_id"`
	RedisDBIndex   *int      `db:"redis_db_index" json:"redis_db_index,omitempty"`
	KeyPrefix      string    `db:"key_prefix" json:"key_prefix"`
	Reason         *string   `db:"reason" json:"reason,omitempty"`
	IsActive       bool      `db:"is_active" json:"is_active"`
	CreatedBy      *uint64   `db:"created_by" json:"created_by,omitempty"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}
