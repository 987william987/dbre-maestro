package model

import "time"

type SessionLoopJob struct {
	ID                uint64     `db:"id" json:"id"`
	RequestedBy       uint64     `db:"requested_by" json:"requested_by"`
	ConnectionID      uint64     `db:"connection_id" json:"connection_id"`
	Engine            string     `db:"engine" json:"engine"`
	TargetMode        string     `db:"target_mode" json:"target_mode"`
	Region            string     `db:"region" json:"region"`
	ClusterID         string     `db:"cluster_id" json:"cluster_id"`
	NodeID            string     `db:"node_id" json:"node_id"`
	TargetHost        string     `db:"target_host" json:"target_host"`
	TargetPort        uint16     `db:"target_port" json:"target_port"`
	TargetKey         string     `db:"target_key" json:"-"`
	ActiveTargetKey   *string    `db:"active_target_key" json:"-"`
	DatabaseName      string     `db:"database_name" json:"database_name"`
	PrefixEncrypted   []byte     `db:"prefix_encrypted" json:"-"`
	PrefixShape       string     `db:"prefix_shape" json:"prefix_shape"`
	PrefixHash        string     `db:"prefix_hash" json:"prefix_hash"`
	MinimumAgeSeconds uint       `db:"minimum_age_seconds" json:"minimum_age_seconds"`
	IntervalSeconds   uint       `db:"interval_seconds" json:"interval_seconds"`
	DurationSeconds   uint       `db:"duration_seconds" json:"duration_seconds"`
	MaxKills          uint       `db:"max_kills" json:"max_kills"`
	Status            string     `db:"status" json:"status"`
	KillCount         uint       `db:"kill_count" json:"kill_count"`
	ConsecutiveErrors uint       `db:"consecutive_errors" json:"consecutive_errors"`
	LastErrorCode     *string    `db:"last_error_code" json:"last_error_code,omitempty"`
	LastErrorMessage  *string    `db:"last_error_message" json:"last_error_message,omitempty"`
	StartedAt         *time.Time `db:"started_at" json:"started_at,omitempty"`
	ExpiresAt         *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	CompletedAt       *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	CreatedAt         time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at" json:"updated_at"`
}

const (
	SessionLoopStatusPending      = "pending"
	SessionLoopStatusRunning      = "running"
	SessionLoopStatusCompleted    = "completed"
	SessionLoopStatusStopped      = "stopped"
	SessionLoopStatusExpired      = "expired"
	SessionLoopStatusLimitReached = "limit_reached"
	SessionLoopStatusFailed       = "failed"
	SessionLoopStatusInterrupted  = "interrupted"
)
