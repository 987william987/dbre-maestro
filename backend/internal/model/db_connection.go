package model

import "time"

type DBConnection struct {
	ID                   uint64    `db:"id"                     json:"id"`
	Name                 string    `db:"name"                   json:"name"`
	DBType               string    `db:"db_type"                json:"db_type"`
	Host                 string    `db:"host"                   json:"host"`
	Port                 uint16    `db:"port"                   json:"port"`
	DatabaseName         *string   `db:"database_name"          json:"database_name,omitempty"`
	Username             string    `db:"username"               json:"username"`
	PasswordEncrypted    []byte    `db:"password_encrypted"     json:"-"`
	EncryptionKeyVersion uint      `db:"encryption_key_version" json:"encryption_key_version"`
	SSLMode              string    `db:"ssl_mode"               json:"ssl_mode"`
	ExtraParams          *string   `db:"extra_params"           json:"extra_params,omitempty"`
	CreatedBy            uint64    `db:"created_by"             json:"created_by"`
	CreatedAt            time.Time `db:"created_at"             json:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"             json:"updated_at"`
}
