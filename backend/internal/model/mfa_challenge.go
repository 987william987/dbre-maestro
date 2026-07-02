package model

import "time"

type MFAChallenge struct {
	ID           uint64     `db:"id"`
	TokenID      string     `db:"token_id"`
	UserID       uint64     `db:"user_id"`
	Setup        bool       `db:"setup"`
	ExpiresAt    time.Time  `db:"expires_at"`
	AttemptCount int        `db:"attempt_count"`
	UsedAt       *time.Time `db:"used_at"`
	RevokedAt    *time.Time `db:"revoked_at"`
	CreatedIP    *string    `db:"created_ip"`
	CreatedAt    time.Time  `db:"created_at"`
}
