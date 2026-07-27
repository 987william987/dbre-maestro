package model

import "time"

type SSOLoginState struct {
	ID              uint64     `db:"id"`
	State           string     `db:"state"`
	ReturnTo        string     `db:"return_to"`
	UserID          *uint64    `db:"user_id"`
	Ticket          string     `db:"ticket"`
	Error           string     `db:"error"`
	IdentityJSON    []byte     `db:"identity_json"`
	ExpiresAt       time.Time  `db:"expires_at"`
	UsedAt          *time.Time `db:"used_at"`
	TicketExpiresAt *time.Time `db:"ticket_expires_at"`
	TicketUsedAt    *time.Time `db:"ticket_used_at"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}
