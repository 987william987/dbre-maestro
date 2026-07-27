package model

import "time"

type Session struct {
	ID           uint64     `db:"id"            json:"id"`
	UserID       uint64     `db:"user_id"       json:"user_id"`
	TokenHash    string     `db:"token_hash"    json:"-"`
	UserAgent    *string    `db:"user_agent"    json:"user_agent,omitempty"`
	IPAddress    *string    `db:"ip_address"    json:"ip_address,omitempty"`
	AuthMethod   string     `db:"auth_method"   json:"auth_method"`
	AuthProvider string     `db:"auth_provider" json:"auth_provider"`
	MFASatisfied bool       `db:"mfa_satisfied" json:"mfa_satisfied"`
	MFASource    string     `db:"mfa_source"    json:"mfa_source"`
	ExpiresAt    time.Time  `db:"expires_at"    json:"expires_at"`
	RevokedAt    *time.Time `db:"revoked_at"    json:"revoked_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
}
