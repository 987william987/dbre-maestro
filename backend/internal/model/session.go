package model

import "time"

type Session struct {
	ID         uint64     `db:"id"`
	UserID     uint64     `db:"user_id"`
	TokenHash  string     `db:"token_hash"`
	UserAgent  *string    `db:"user_agent"`
	IPAddress  *string    `db:"ip_address"`
	ExpiresAt  time.Time  `db:"expires_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
	CreatedAt  time.Time  `db:"created_at"`
}
