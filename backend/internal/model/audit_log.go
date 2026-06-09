package model

import (
	"encoding/json"
	"time"
)

// AuditLog is a read model for scanning rows from audit_logs.
// Writes go through repository.AuditRepo.Log (append-only).
type AuditLog struct {
	ID           uint64          `db:"id"            json:"id"`
	ActorID      *uint64         `db:"actor_id"      json:"actor_id,omitempty"`
	ActorName    string          `db:"actor_name"    json:"actor_name"`
	ActionType   string          `db:"action_type"   json:"action_type"`
	ResourceType *string         `db:"resource_type" json:"resource_type,omitempty"`
	ResourceID   *uint64         `db:"resource_id"   json:"resource_id,omitempty"`
	Details      json.RawMessage `db:"details"       json:"details,omitempty"`
	IPAddress    *string         `db:"ip_address"    json:"ip_address,omitempty"`
	CreatedAt    time.Time       `db:"created_at"    json:"created_at"`
}
