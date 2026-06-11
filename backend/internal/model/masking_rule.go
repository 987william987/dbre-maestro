package model

import "time"

type MaskingRule struct {
	ID         uint64    `db:"id" json:"id"`
	ColumnName string    `db:"column_name" json:"column_name"`
	MaskMode   string    `db:"mask_mode" json:"mask_mode"`
	CreatedBy  uint64    `db:"created_by" json:"created_by"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}
