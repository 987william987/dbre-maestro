package model

import (
	"encoding/json"
	"time"
)

type MaskingRule struct {
	ID         uint64          `db:"id" json:"id"`
	ColumnName string          `db:"column_name" json:"column_name"`
	MatchType  string          `db:"match_type" json:"match_type"`
	MaskMode   string          `db:"mask_mode" json:"mask_mode"`
	MaskConfig json.RawMessage `db:"mask_config" json:"mask_config"`
	Enabled    bool            `db:"enabled" json:"enabled"`
	CreatedBy  uint64          `db:"created_by" json:"created_by"`
	CreatedAt  time.Time       `db:"created_at" json:"created_at"`
}
