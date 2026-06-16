package model

import "time"

type DBConnection struct {
	ID                   uint64                   `db:"id"                     json:"id"`
	Name                 string                   `db:"name"                   json:"name"`
	DBType               string                   `db:"db_type"                json:"db_type"`
	Host                 string                   `db:"host"                   json:"host"`
	Port                 uint16                   `db:"port"                   json:"port"`
	DatabaseName         *string                  `db:"database_name"          json:"database_name,omitempty"`
	Username             string                   `db:"username"               json:"username"`
	PasswordEncrypted    []byte                   `db:"password_encrypted"     json:"-"`
	EncryptionKeyVersion uint                     `db:"encryption_key_version" json:"encryption_key_version"`
	SSLMode              string                   `db:"ssl_mode"               json:"ssl_mode"`
	ExtraParams          *string                  `db:"extra_params"           json:"extra_params,omitempty"`
	LastTestStatus       *string                  `db:"last_test_status"       json:"last_test_status,omitempty"`
	LastTestError        *string                  `db:"last_test_error"        json:"last_test_error,omitempty"`
	LastTestedAt         *time.Time               `db:"last_tested_at"         json:"last_tested_at,omitempty"`
	CreatedBy            uint64                   `db:"created_by"             json:"created_by"`
	CreatedAt            time.Time                `db:"created_at"             json:"created_at"`
	UpdatedAt            time.Time                `db:"updated_at"             json:"updated_at"`
	Credentials          []DBConnectionCredential `db:"-" json:"credentials,omitempty"`
}

const (
	DBCredentialRoleReadonly  = "readonly"
	DBCredentialRoleReadwrite = "readwrite"
)

type DBConnectionCredential struct {
	ID                   uint64    `db:"id"                     json:"id"`
	DBConnectionID       uint64    `db:"db_connection_id"       json:"db_connection_id"`
	CredentialRole       string    `db:"credential_role"        json:"credential_role"`
	Username             string    `db:"username"               json:"username"`
	PasswordEncrypted    []byte    `db:"password_encrypted"     json:"-"`
	EncryptionKeyVersion uint      `db:"encryption_key_version" json:"encryption_key_version"`
	CreatedAt            time.Time `db:"created_at"             json:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"             json:"updated_at"`
	HasPassword          bool      `db:"-"                      json:"has_password"`
}

type DBConnectionCredentialInput struct {
	CredentialRole string
	Username       string
	Password       string
}
