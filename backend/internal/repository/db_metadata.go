package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type DBMetadataRepo struct {
	db *sqlx.DB
}

func NewDBMetadataRepo(db *sqlx.DB) *DBMetadataRepo {
	return &DBMetadataRepo{db: db}
}

func (r *DBMetadataRepo) ListInventorySnapshots(ctx context.Context, engine string, limit int) ([]model.CloudDBInventorySnapshot, error) {
	if limit <= 0 {
		limit = 200
	}

	baseQuery := `SELECT
		id,
		snapshot_at,
		provider,
		engine,
		region,
		az,
		account_id,
		db_identifier,
		cluster_identifier,
		instance_identifier,
		role,
		engine_version,
		instance_class,
		storage_type,
		cluster_endpoint,
		cluster_reader_endpoint,
		instance_endpoint,
		raw_payload_json
	FROM cloud_db_inventory_snapshots`
	args := []any{}
	if trimmedEngine := strings.TrimSpace(engine); trimmedEngine != "" {
		baseQuery += ` WHERE engine = ?`
		args = append(args, trimmedEngine)
	}
	baseQuery += ` ORDER BY snapshot_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	var items []model.CloudDBInventorySnapshot
	if err := r.db.SelectContext(ctx, &items, baseQuery, args...); err != nil {
		return nil, fmt.Errorf("list cloud_db_inventory_snapshots: %w", err)
	}
	return items, nil
}

func (r *DBMetadataRepo) ListObjectSnapshots(ctx context.Context, connectionID uint64, limit int) ([]model.DBObjectSnapshot, error) {
	if limit <= 0 {
		limit = 500
	}

	baseQuery := `SELECT
		id,
		snapshot_at,
		db_connection_id,
		connection_name_snapshot,
		engine,
		cluster_name,
		node_name,
		database_name,
		schema_name,
		table_name,
		data_size_bytes,
		index_size_bytes
	FROM db_object_snapshots`
	args := []any{}
	if connectionID > 0 {
		baseQuery += ` WHERE db_connection_id = ?`
		args = append(args, connectionID)
	}
	baseQuery += ` ORDER BY snapshot_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	var items []model.DBObjectSnapshot
	if err := r.db.SelectContext(ctx, &items, baseQuery, args...); err != nil {
		return nil, fmt.Errorf("list db_object_snapshots: %w", err)
	}
	return items, nil
}

func (r *DBMetadataRepo) ReplaceInventorySnapshots(ctx context.Context, snapshotAt time.Time, items []model.CloudDBInventorySnapshot) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace inventory snapshots tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM cloud_db_inventory_snapshots`); err != nil {
		return fmt.Errorf("clear cloud_db_inventory_snapshots: %w", err)
	}

	for _, item := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO cloud_db_inventory_snapshots
			 (snapshot_at, provider, engine, region, az, account_id, db_identifier, cluster_identifier, instance_identifier, role, engine_version, instance_class, storage_type, cluster_endpoint, cluster_reader_endpoint, instance_endpoint, raw_payload_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotAt,
			item.Provider,
			item.Engine,
			item.Region,
			item.AZ,
			item.AccountID,
			item.DBIdentifier,
			item.ClusterIdentifier,
			item.InstanceIdentifier,
			item.Role,
			item.EngineVersion,
			item.InstanceClass,
			item.StorageType,
			item.ClusterEndpoint,
			item.ClusterReaderEndpoint,
			item.InstanceEndpoint,
			item.RawPayloadJSON,
		); err != nil {
			return fmt.Errorf("insert inventory snapshot %s: %w", item.DBIdentifier, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace inventory snapshots tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *DBMetadataRepo) ReplaceObjectSnapshotsForConnection(ctx context.Context, snapshotAt time.Time, dbConnectionID uint64, items []model.DBObjectSnapshot) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace object snapshots tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM db_object_snapshots WHERE db_connection_id = ?`, dbConnectionID); err != nil {
		return fmt.Errorf("clear db_object_snapshots for connection %d: %w", dbConnectionID, err)
	}

	for _, item := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO db_object_snapshots
			 (snapshot_at, db_connection_id, connection_name_snapshot, engine, cluster_name, node_name, database_name, schema_name, table_name, data_size_bytes, index_size_bytes)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotAt,
			dbConnectionID,
			item.ConnectionName,
			item.Engine,
			item.ClusterName,
			item.NodeName,
			item.DatabaseName,
			item.SchemaName,
			item.TableName,
			item.DataSizeBytes,
			item.IndexSizeBytes,
		); err != nil {
			return fmt.Errorf("insert object snapshot %s.%s.%s: %w", item.DatabaseName, item.SchemaName, item.TableName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace object snapshots tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *DBMetadataRepo) FindLatestInventoryByEndpoint(ctx context.Context, endpoint string) (*model.CloudDBInventorySnapshot, error) {
	trimmedEndpoint := strings.TrimSpace(endpoint)
	if trimmedEndpoint == "" {
		return nil, nil
	}

	var item model.CloudDBInventorySnapshot
	err := r.db.GetContext(ctx, &item, `SELECT
		id,
		snapshot_at,
		provider,
		engine,
		region,
		az,
		account_id,
		db_identifier,
		cluster_identifier,
		instance_identifier,
		role,
		engine_version,
		instance_class,
		storage_type,
		cluster_endpoint,
		cluster_reader_endpoint,
		instance_endpoint,
		raw_payload_json
	FROM cloud_db_inventory_snapshots
	WHERE cluster_endpoint = ?
	   OR cluster_reader_endpoint = ?
	   OR instance_endpoint = ?
	ORDER BY snapshot_at DESC, id DESC
	LIMIT 1`, trimmedEndpoint, trimmedEndpoint, trimmedEndpoint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find inventory by endpoint %s: %w", trimmedEndpoint, err)
	}
	return &item, nil
}
