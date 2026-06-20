package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type DBMetadataRepo struct {
	db *sqlx.DB
}

func NewDBMetadataRepo(db *sqlx.DB) *DBMetadataRepo {
	return &DBMetadataRepo{db: db}
}

func (r *DBMetadataRepo) GetJobRun(ctx context.Context, jobName string) (*model.DBMetadataJobRun, error) {
	var run model.DBMetadataJobRun
	err := r.db.GetContext(ctx, &run, `SELECT job_name, last_scheduled_at, last_started_at, last_finished_at, last_success_at, status, error_message, updated_at FROM db_metadata_job_runs WHERE job_name = ?`, jobName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get db metadata job run %s: %w", jobName, err)
	}
	return &run, nil
}

func (r *DBMetadataRepo) MarkJobStarted(ctx context.Context, jobName string, scheduledAt time.Time) error {
	now := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO db_metadata_job_runs (job_name, last_scheduled_at, last_started_at, last_finished_at, last_success_at, status, error_message, updated_at)
		 VALUES (?, ?, ?, NULL, NULL, 'running', NULL, ?)
		 ON DUPLICATE KEY UPDATE last_scheduled_at = VALUES(last_scheduled_at), last_started_at = VALUES(last_started_at), status = 'running', error_message = NULL, updated_at = VALUES(updated_at)`,
		jobName, scheduledAt.UTC(), now, now,
	)
	if err != nil {
		return fmt.Errorf("mark db metadata job started %s: %w", jobName, err)
	}
	return nil
}

func (r *DBMetadataRepo) MarkJobFinished(ctx context.Context, jobName string, success bool, message string) error {
	now := timeutil.NowUTC()
	status := "success"
	var successAt *time.Time = &now
	var errorMessage *string
	if !success {
		status = "failed"
		successAt = nil
		if strings.TrimSpace(message) != "" {
			errorMessage = &message
		}
	}
	_, err := r.db.ExecContext(ctx,
		`UPDATE db_metadata_job_runs
		 SET last_finished_at = ?, last_success_at = COALESCE(?, last_success_at), status = ?, error_message = ?, updated_at = ?
		 WHERE job_name = ?`,
		now, successAt, status, errorMessage, now, jobName,
	)
	if err != nil {
		return fmt.Errorf("mark db metadata job finished %s: %w", jobName, err)
	}
	return nil
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
	baseQuery += ` ORDER BY snapshot_at DESC, id DESC`
	if limit > 0 {
		baseQuery += ` LIMIT ?`
		args = append(args, limit)
	}

	var items []model.DBObjectSnapshot
	if err := r.db.SelectContext(ctx, &items, baseQuery, args...); err != nil {
		return nil, fmt.Errorf("list db_object_snapshots: %w", err)
	}
	return items, nil
}

func (r *DBMetadataRepo) DeleteObjectSnapshotsForConnection(ctx context.Context, connectionID uint64) error {
	if connectionID == 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM db_object_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete db_object_snapshots for connection %d: %w", connectionID, err)
	}
	return nil
}

func (r *DBMetadataRepo) DeleteObjectSnapshotsExceptConnectionIDs(ctx context.Context, connectionIDs []uint64) error {
	if len(connectionIDs) == 0 {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM db_object_snapshots`); err != nil {
			return fmt.Errorf("delete all db_object_snapshots: %w", err)
		}
		return nil
	}

	query, args, err := sqlx.In(`DELETE FROM db_object_snapshots WHERE db_connection_id NOT IN (?)`, connectionIDs)
	if err != nil {
		return fmt.Errorf("build delete db_object_snapshots except ids query: %w", err)
	}
	query = r.db.Rebind(query)
	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete db_object_snapshots except ids: %w", err)
	}
	return nil
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
			 (snapshot_at, provider, engine, region, az, account_id, db_identifier, cluster_identifier, instance_identifier, role, engine_version, instance_class, storage_type, cluster_endpoint, cluster_reader_endpoint, instance_endpoint, raw_payload_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotAt.UTC(),
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
			timeutil.NowUTC(),
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
			 (snapshot_at, db_connection_id, connection_name_snapshot, engine, cluster_name, node_name, database_name, schema_name, table_name, data_size_bytes, index_size_bytes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotAt.UTC(),
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
			timeutil.NowUTC(),
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
