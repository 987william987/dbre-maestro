package model

import "time"

type SQLReviewRule struct {
	ID          uint64     `db:"id"          json:"id"`
	RuleName    string     `db:"rule_name"   json:"rule_name"`
	Enabled     bool       `db:"enabled"     json:"enabled"`
	Threshold   *int64     `db:"threshold"   json:"threshold,omitempty"`
	Description string     `db:"description" json:"description"`
	UpdatedBy   *uint64    `db:"updated_by"  json:"updated_by,omitempty"`
	UpdatedAt   time.Time  `db:"updated_at"  json:"updated_at"`
}
