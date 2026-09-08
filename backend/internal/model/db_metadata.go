package model

import "time"

type CloudDBInventorySnapshot struct {
	ID                    uint64            `db:"id"                      json:"id"`
	SnapshotAt            time.Time         `db:"snapshot_at"             json:"snapshot_at"`
	Provider              string            `db:"provider"                json:"provider"`
	Engine                string            `db:"engine"                  json:"engine"`
	Region                string            `db:"region"                  json:"region"`
	AZ                    *string           `db:"az"                      json:"az,omitempty"`
	AccountID             *string           `db:"account_id"              json:"account_id,omitempty"`
	DBIdentifier          string            `db:"db_identifier"           json:"db_identifier"`
	ClusterIdentifier     *string           `db:"cluster_identifier"      json:"cluster_identifier,omitempty"`
	InstanceIdentifier    *string           `db:"instance_identifier"     json:"instance_identifier,omitempty"`
	Role                  *string           `db:"role"                    json:"role,omitempty"`
	EngineVersion         *string           `db:"engine_version"          json:"engine_version,omitempty"`
	InstanceClass         *string           `db:"instance_class"          json:"instance_class,omitempty"`
	StorageType           *string           `db:"storage_type"            json:"storage_type,omitempty"`
	ClusterEndpoint       *string           `db:"cluster_endpoint"        json:"cluster_endpoint,omitempty"`
	ClusterReaderEndpoint *string           `db:"cluster_reader_endpoint" json:"cluster_reader_endpoint,omitempty"`
	InstanceEndpoint      *string           `db:"instance_endpoint"       json:"instance_endpoint,omitempty"`
	RawPayloadJSON        *string           `db:"raw_payload_json"        json:"raw_payload_json,omitempty"`
	TagsJSON              *string           `db:"tags_json"               json:"-"`
	Tags                  map[string]string `db:"-"                json:"tags,omitempty"`
	MappingStatus         string            `db:"-"                       json:"mapping_status"`
	MappingConnections    []string          `db:"-"                       json:"mapping_connections,omitempty"`
}

type DBObjectSnapshot struct {
	ID             uint64    `db:"id"                     json:"id"`
	SnapshotAt     time.Time `db:"snapshot_at"            json:"snapshot_at"`
	DBConnectionID uint64    `db:"db_connection_id"       json:"db_connection_id"`
	ConnectionName string    `db:"connection_name_snapshot" json:"connection_name"`
	Engine         string    `db:"engine"                 json:"engine"`
	ClusterName    *string   `db:"cluster_name"           json:"cluster_name,omitempty"`
	NodeName       *string   `db:"node_name"              json:"node_name,omitempty"`
	DatabaseName   string    `db:"database_name"          json:"database_name"`
	SchemaName     string    `db:"schema_name"            json:"schema_name"`
	TableName      string    `db:"table_name"             json:"table_name"`
	RowCount       int64     `db:"row_count"              json:"row_count"`
	DataSizeBytes  int64     `db:"data_size_bytes"        json:"data_size_bytes"`
	IndexSizeBytes int64     `db:"index_size_bytes"       json:"index_size_bytes"`
}

type DBDatabaseSnapshot struct {
	ID               uint64    `db:"id" json:"id"`
	SnapshotAt       time.Time `db:"snapshot_at" json:"snapshot_at"`
	DBConnectionID   uint64    `db:"db_connection_id" json:"db_connection_id"`
	Engine           string    `db:"engine" json:"engine"`
	DatabaseName     string    `db:"database_name" json:"database_name"`
	CharacterSetName *string   `db:"character_set_name" json:"character_set_name,omitempty"`
	CollationName    *string   `db:"collation_name" json:"collation_name,omitempty"`
	TableCount       int64     `db:"table_count" json:"table_count"`
	DataSizeBytes    int64     `db:"data_size_bytes" json:"data_size_bytes"`
	IndexSizeBytes   int64     `db:"index_size_bytes" json:"index_size_bytes"`
}

type DBAccountSnapshot struct {
	ID             uint64     `db:"id" json:"id"`
	SnapshotAt     time.Time  `db:"snapshot_at" json:"snapshot_at"`
	DBConnectionID uint64     `db:"db_connection_id" json:"db_connection_id"`
	Engine         string     `db:"engine" json:"engine"`
	PrincipalKey   string     `db:"principal_key" json:"principal_key"`
	PrincipalName  string     `db:"principal_name" json:"principal_name"`
	PrincipalHost  *string    `db:"principal_host" json:"principal_host,omitempty"`
	PrincipalType  string     `db:"principal_type" json:"principal_type"`
	CanLogin       bool       `db:"can_login" json:"can_login"`
	IsSuperuser    bool       `db:"is_superuser" json:"is_superuser"`
	InheritsRoles  bool       `db:"inherits_roles" json:"inherits_roles"`
	CanCreateRole  bool       `db:"can_create_role" json:"can_create_role"`
	CanCreateDB    bool       `db:"can_create_database" json:"can_create_database"`
	CanReplicate   bool       `db:"can_replicate" json:"can_replicate"`
	CanBypassRLS   bool       `db:"can_bypass_rls" json:"can_bypass_rls"`
	IsLocked       bool       `db:"is_locked" json:"is_locked"`
	ValidUntil     *time.Time `db:"valid_until" json:"valid_until,omitempty"`
}

type DBAccountGrantSnapshot struct {
	ID             uint64    `db:"id" json:"id"`
	SnapshotAt     time.Time `db:"snapshot_at" json:"snapshot_at"`
	DBConnectionID uint64    `db:"db_connection_id" json:"db_connection_id"`
	PrincipalKey   string    `db:"principal_key" json:"principal_key"`
	GrantKind      string    `db:"grant_kind" json:"grant_kind"`
	GrantStatement *string   `db:"grant_statement" json:"grant_statement,omitempty"`
	GrantedRole    *string   `db:"granted_role" json:"granted_role,omitempty"`
	ScopeType      *string   `db:"scope_type" json:"scope_type,omitempty"`
	DatabaseName   *string   `db:"database_name" json:"database_name,omitempty"`
	SchemaName     *string   `db:"schema_name" json:"schema_name,omitempty"`
	ObjectName     *string   `db:"object_name" json:"object_name,omitempty"`
	PrivilegeType  *string   `db:"privilege_type" json:"privilege_type,omitempty"`
	IsGrantable    bool      `db:"is_grantable" json:"is_grantable"`
}

type DBAccountSnapshotStatus struct {
	DBConnectionID uint64     `db:"db_connection_id" json:"db_connection_id"`
	LastAttemptAt  time.Time  `db:"last_attempt_at" json:"last_attempt_at"`
	LastSuccessAt  *time.Time `db:"last_success_at" json:"last_success_at,omitempty"`
	Status         string     `db:"status" json:"status"`
	ErrorMessage   *string    `db:"error_message" json:"error_message,omitempty"`
}

type DBMetadataJobRun struct {
	JobName         string     `db:"job_name" json:"job_name"`
	LastScheduledAt *time.Time `db:"last_scheduled_at" json:"last_scheduled_at,omitempty"`
	LastStartedAt   *time.Time `db:"last_started_at" json:"last_started_at,omitempty"`
	LastFinishedAt  *time.Time `db:"last_finished_at" json:"last_finished_at,omitempty"`
	LastSuccessAt   *time.Time `db:"last_success_at" json:"last_success_at,omitempty"`
	Status          string     `db:"status" json:"status"`
	ErrorMessage    *string    `db:"error_message" json:"error_message,omitempty"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}
