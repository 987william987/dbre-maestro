package model

import "time"

type Notification struct {
	ID           uint64     `db:"id"            json:"id"`
	UserID       uint64     `db:"user_id"       json:"user_id"`
	Type         string     `db:"type"          json:"type"`
	Title        string     `db:"title"         json:"title"`
	Body         string     `db:"body"          json:"body"`
	ResourceType *string    `db:"resource_type" json:"resource_type,omitempty"`
	ResourceID   *uint64    `db:"resource_id"   json:"resource_id,omitempty"`
	IsRead       bool       `db:"is_read"       json:"is_read"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
}
