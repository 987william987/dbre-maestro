package repository

import (
	"context"
	"database/sql"
	"encoding/json"
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

type DBMetadataObjectHealthStats struct {
	SnapshotConnectionCount int64 `db:"snapshot_connection_count" json:"snapshot_connection_count"`
	StaleConnectionCount    int64 `db:"stale_connection_count" json:"stale_connection_count"`
	ObjectCount             int64 `db:"object_count" json:"object_count"`
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

func (r *DBMetadataRepo) ObjectHealthStats(ctx context.Context, staleBefore time.Time) (*DBMetadataObjectHealthStats, error) {
	stats := &DBMetadataObjectHealthStats{}
	if err := r.db.GetContext(ctx, stats,
		`SELECT
		 COUNT(DISTINCT db_connection_id) AS snapshot_connection_count,
		 COUNT(DISTINCT CASE WHEN snapshot_at < ? THEN db_connection_id END) AS stale_connection_count,
		 COUNT(*) AS object_count
		 FROM db_object_snapshots`,
		staleBefore,
	); err != nil {
		return nil, fmt.Errorf("load db metadata object health stats: %w", err)
	}
	return stats, nil
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
		raw_payload_json,
		tags_json
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
	hydrateInventoryTags(items)
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
		row_count,
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

func (r *DBMetadataRepo) ListDatabaseSummaries(ctx context.Context, connectionID uint64) ([]model.DBDatabaseSnapshot, error) {
	items := make([]model.DBDatabaseSnapshot, 0)
	if err := r.db.SelectContext(ctx, &items, `SELECT id, snapshot_at, db_connection_id, engine, database_name, character_set_name, collation_name, table_count, data_size_bytes, index_size_bytes FROM db_database_snapshots WHERE db_connection_id = ? ORDER BY database_name`, connectionID); err != nil {
		return nil, fmt.Errorf("list database summaries for connection %d: %w", connectionID, err)
	}
	return items, nil
}

func (r *DBMetadataRepo) ListAccountSnapshots(ctx context.Context, connectionID uint64) ([]model.DBAccountSnapshot, []model.DBAccountGrantSnapshot, *model.DBAccountSnapshotStatus, error) {
	accounts := make([]model.DBAccountSnapshot, 0)
	if err := r.db.SelectContext(ctx, &accounts, `SELECT id, snapshot_at, db_connection_id, engine, principal_key, principal_name, principal_host, principal_type, can_login, is_superuser, inherits_roles, can_create_role, can_create_database, can_replicate, can_bypass_rls, is_locked, valid_until FROM db_account_snapshots WHERE db_connection_id = ? ORDER BY principal_name, principal_host`, connectionID); err != nil {
		return nil, nil, nil, fmt.Errorf("list account snapshots for connection %d: %w", connectionID, err)
	}
	grants := make([]model.DBAccountGrantSnapshot, 0)
	if err := r.db.SelectContext(ctx, &grants, `SELECT id, snapshot_at, db_connection_id, principal_key, grant_kind, grant_statement, granted_role, scope_type, database_name, schema_name, object_name, privilege_type, is_grantable FROM db_account_grant_snapshots WHERE db_connection_id = ? ORDER BY principal_key, grant_kind, scope_type, database_name, schema_name, object_name, privilege_type`, connectionID); err != nil {
		return nil, nil, nil, fmt.Errorf("list account grant snapshots for connection %d: %w", connectionID, err)
	}
	var status model.DBAccountSnapshotStatus
	err := r.db.GetContext(ctx, &status, `SELECT db_connection_id, last_attempt_at, last_success_at, status, error_message FROM db_account_snapshot_statuses WHERE db_connection_id = ?`, connectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts, grants, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get account snapshot status for connection %d: %w", connectionID, err)
	}
	return accounts, grants, &status, nil
}

func (r *DBMetadataRepo) ReplaceAccountSnapshots(ctx context.Context, connectionID uint64, snapshotAt time.Time, accounts []model.DBAccountSnapshot, grants []model.DBAccountGrantSnapshot) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace account snapshots tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_account_grant_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_account_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return err
	}
	for _, item := range accounts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO db_account_snapshots (snapshot_at, db_connection_id, engine, principal_key, principal_name, principal_host, principal_type, can_login, is_superuser, inherits_roles, can_create_role, can_create_database, can_replicate, can_bypass_rls, is_locked, valid_until) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshotAt.UTC(), connectionID, item.Engine, item.PrincipalKey, item.PrincipalName, item.PrincipalHost, item.PrincipalType, item.CanLogin, item.IsSuperuser, item.InheritsRoles, item.CanCreateRole, item.CanCreateDB, item.CanReplicate, item.CanBypassRLS, item.IsLocked, item.ValidUntil); err != nil {
			return fmt.Errorf("insert account snapshot %s: %w", item.PrincipalKey, err)
		}
	}
	for _, item := range grants {
		if _, err := tx.ExecContext(ctx, `INSERT INTO db_account_grant_snapshots (snapshot_at, db_connection_id, principal_key, grant_kind, grant_statement, granted_role, scope_type, database_name, schema_name, object_name, privilege_type, is_grantable) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshotAt.UTC(), connectionID, item.PrincipalKey, item.GrantKind, item.GrantStatement, item.GrantedRole, item.ScopeType, item.DatabaseName, item.SchemaName, item.ObjectName, item.PrivilegeType, item.IsGrantable); err != nil {
			return fmt.Errorf("insert account grant snapshot %s: %w", item.PrincipalKey, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO db_account_snapshot_statuses (db_connection_id, last_attempt_at, last_success_at, status, error_message) VALUES (?, ?, ?, 'success', NULL) ON DUPLICATE KEY UPDATE last_attempt_at = VALUES(last_attempt_at), last_success_at = VALUES(last_success_at), status = 'success', error_message = NULL`, connectionID, snapshotAt.UTC(), snapshotAt.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DBMetadataRepo) MarkAccountSnapshotFailed(ctx context.Context, connectionID uint64, attemptedAt time.Time, message string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO db_account_snapshot_statuses (db_connection_id, last_attempt_at, last_success_at, status, error_message) VALUES (?, ?, NULL, 'failed', ?) ON DUPLICATE KEY UPDATE last_attempt_at = VALUES(last_attempt_at), status = 'failed', error_message = VALUES(error_message)`, connectionID, attemptedAt.UTC(), message)
	return err
}

func (r *DBMetadataRepo) FindObjectSnapshot(ctx context.Context, connectionID uint64, databaseName, schemaName, tableName string) (*model.DBObjectSnapshot, error) {
	var item model.DBObjectSnapshot
	err := r.db.GetContext(ctx, &item, `SELECT
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
		row_count,
		data_size_bytes,
		index_size_bytes
	FROM db_object_snapshots
	WHERE db_connection_id = ?
	  AND database_name = ?
	  AND schema_name = ?
	  AND table_name = ?
	ORDER BY snapshot_at DESC, id DESC
	LIMIT 1`, connectionID, databaseName, schemaName, tableName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find db_object_snapshot %d %s.%s.%s: %w", connectionID, databaseName, schemaName, tableName, err)
	}
	return &item, nil
}

func (r *DBMetadataRepo) DeleteObjectSnapshotsForConnection(ctx context.Context, connectionID uint64) error {
	if connectionID == 0 {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM db_object_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete db_object_snapshots for connection %d: %w", connectionID, err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM db_database_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete db_database_snapshots for connection %d: %w", connectionID, err)
	}
	return nil
}

func (r *DBMetadataRepo) ReplaceDatabaseSnapshotsForConnection(ctx context.Context, snapshotAt time.Time, connectionID uint64, items []model.DBDatabaseSnapshot) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_database_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO db_database_snapshots (snapshot_at, db_connection_id, engine, database_name, character_set_name, collation_name, table_count, data_size_bytes, index_size_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshotAt.UTC(), connectionID, item.Engine, item.DatabaseName, item.CharacterSetName, item.CollationName, item.TableCount, item.DataSizeBytes, item.IndexSizeBytes); err != nil {
			return fmt.Errorf("insert database snapshot %s: %w", item.DatabaseName, err)
		}
	}
	return tx.Commit()
}

func (r *DBMetadataRepo) DeleteObjectSnapshotsExceptConnectionIDs(ctx context.Context, connectionIDs []uint64) error {
	if len(connectionIDs) == 0 {
		if _, err := r.db.ExecContext(ctx, `DELETE FROM db_object_snapshots`); err != nil {
			return fmt.Errorf("delete all db_object_snapshots: %w", err)
		}
		if _, err := r.db.ExecContext(ctx, `DELETE FROM db_database_snapshots`); err != nil {
			return fmt.Errorf("delete all db_database_snapshots: %w", err)
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
	databaseQuery, databaseArgs, err := sqlx.In(`DELETE FROM db_database_snapshots WHERE db_connection_id NOT IN (?)`, connectionIDs)
	if err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, r.db.Rebind(databaseQuery), databaseArgs...); err != nil {
		return fmt.Errorf("delete db_database_snapshots except ids: %w", err)
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
			 (snapshot_at, provider, engine, region, az, account_id, db_identifier, cluster_identifier, instance_identifier, role, engine_version, instance_class, storage_type, cluster_endpoint, cluster_reader_endpoint, instance_endpoint, raw_payload_json, tags_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
			nullableTagsJSON(item.Tags),
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
			 (snapshot_at, db_connection_id, connection_name_snapshot, engine, cluster_name, node_name, database_name, schema_name, table_name, row_count, data_size_bytes, index_size_bytes, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshotAt.UTC(),
			dbConnectionID,
			item.ConnectionName,
			item.Engine,
			item.ClusterName,
			item.NodeName,
			item.DatabaseName,
			item.SchemaName,
			item.TableName,
			item.RowCount,
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

func (r *DBMetadataRepo) ReplaceObjectAndDatabaseSnapshotsForConnection(ctx context.Context, snapshotAt time.Time, connectionID uint64, objects []model.DBObjectSnapshot, databases []model.DBDatabaseSnapshot) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace object and database snapshots tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_object_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_database_snapshots WHERE db_connection_id = ?`, connectionID); err != nil {
		return err
	}
	for _, item := range objects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO db_object_snapshots (snapshot_at, db_connection_id, connection_name_snapshot, engine, cluster_name, node_name, database_name, schema_name, table_name, row_count, data_size_bytes, index_size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshotAt.UTC(), connectionID, item.ConnectionName, item.Engine, item.ClusterName, item.NodeName, item.DatabaseName, item.SchemaName, item.TableName, item.RowCount, item.DataSizeBytes, item.IndexSizeBytes, timeutil.NowUTC()); err != nil {
			return fmt.Errorf("insert object snapshot %s.%s.%s: %w", item.DatabaseName, item.SchemaName, item.TableName, err)
		}
	}
	for _, item := range databases {
		if _, err := tx.ExecContext(ctx, `INSERT INTO db_database_snapshots (snapshot_at, db_connection_id, engine, database_name, character_set_name, collation_name, table_count, data_size_bytes, index_size_bytes) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, snapshotAt.UTC(), connectionID, item.Engine, item.DatabaseName, item.CharacterSetName, item.CollationName, item.TableCount, item.DataSizeBytes, item.IndexSizeBytes); err != nil {
			return fmt.Errorf("insert database snapshot %s: %w", item.DatabaseName, err)
		}
	}
	return tx.Commit()
}

func (r *DBMetadataRepo) DeleteAccountSnapshotsExceptConnectionIDs(ctx context.Context, connectionIDs []uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tables := []string{"db_account_grant_snapshots", "db_account_snapshots", "db_account_snapshot_statuses"}
	for _, table := range tables {
		query := `DELETE FROM ` + table
		args := []any{}
		if len(connectionIDs) > 0 {
			query, args, err = sqlx.In(query+` WHERE db_connection_id NOT IN (?)`, connectionIDs)
			if err != nil {
				return err
			}
			query = r.db.Rebind(query)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("clear stale %s: %w", table, err)
		}
	}
	return tx.Commit()
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
		raw_payload_json,
		tags_json
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
	items := []model.CloudDBInventorySnapshot{item}
	hydrateInventoryTags(items)
	item = items[0]
	return &item, nil
}

func hydrateInventoryTags(items []model.CloudDBInventorySnapshot) {
	for i := range items {
		if items[i].TagsJSON == nil || strings.TrimSpace(*items[i].TagsJSON) == "" {
			continue
		}
		var tags map[string]string
		if err := json.Unmarshal([]byte(*items[i].TagsJSON), &tags); err == nil {
			items[i].Tags = tags
		}
	}
}

func nullableTagsJSON(tags map[string]string) any {
	if len(tags) == 0 {
		return nil
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return nil
	}
	return string(raw)
}
