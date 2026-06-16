package model

import "time"

type CloudDBInventorySnapshot struct {
	ID                    uint64    `db:"id"                      json:"id"`
	SnapshotAt            time.Time `db:"snapshot_at"             json:"snapshot_at"`
	Provider              string    `db:"provider"                json:"provider"`
	Engine                string    `db:"engine"                  json:"engine"`
	Region                string    `db:"region"                  json:"region"`
	AZ                    *string   `db:"az"                      json:"az,omitempty"`
	AccountID             *string   `db:"account_id"              json:"account_id,omitempty"`
	DBIdentifier          string    `db:"db_identifier"           json:"db_identifier"`
	ClusterIdentifier     *string   `db:"cluster_identifier"      json:"cluster_identifier,omitempty"`
	InstanceIdentifier    *string   `db:"instance_identifier"     json:"instance_identifier,omitempty"`
	Role                  *string   `db:"role"                    json:"role,omitempty"`
	EngineVersion         *string   `db:"engine_version"          json:"engine_version,omitempty"`
	InstanceClass         *string   `db:"instance_class"          json:"instance_class,omitempty"`
	StorageType           *string   `db:"storage_type"            json:"storage_type,omitempty"`
	ClusterEndpoint       *string   `db:"cluster_endpoint"        json:"cluster_endpoint,omitempty"`
	ClusterReaderEndpoint *string   `db:"cluster_reader_endpoint" json:"cluster_reader_endpoint,omitempty"`
	InstanceEndpoint      *string   `db:"instance_endpoint"       json:"instance_endpoint,omitempty"`
	RawPayloadJSON        *string   `db:"raw_payload_json"        json:"raw_payload_json,omitempty"`
	MappingStatus         string    `db:"-"                       json:"mapping_status"`
	MappingConnections    []string  `db:"-"                       json:"mapping_connections,omitempty"`
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
	DataSizeBytes  int64     `db:"data_size_bytes"        json:"data_size_bytes"`
	IndexSizeBytes int64     `db:"index_size_bytes"       json:"index_size_bytes"`
}
