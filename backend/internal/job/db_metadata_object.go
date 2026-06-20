package job

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
)

const (
	objectSchedulerPollInterval      = time.Minute
	objectJobConnectionWorkers       = 4
	postgresReservedDatabaseRDSAdmin = "rdsadmin"
	objectJobName                    = "db_metadata_object"
)

func shouldSkipPostgresMetadataDatabase(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), postgresReservedDatabaseRDSAdmin)
}

type DBMetadataObjectJob struct {
	settings  *repository.SettingsRepo
	dbConns   *repository.DBConnectionRepo
	snapshots *repository.DBMetadataRepo
	logger    *slog.Logger

	mu        sync.Mutex
	isRunning bool
}

func NewDBMetadataObjectJob(
	settings *repository.SettingsRepo,
	dbConns *repository.DBConnectionRepo,
	snapshots *repository.DBMetadataRepo,
	logger *slog.Logger,
) *DBMetadataObjectJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &DBMetadataObjectJob{
		settings:  settings,
		dbConns:   dbConns,
		snapshots: snapshots,
		logger:    logger,
	}
}

func (j *DBMetadataObjectJob) Start(ctx context.Context) {
	ticker := time.NewTicker(objectSchedulerPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.runIfDue(ctx)
		}
	}
}

func (j *DBMetadataObjectJob) runIfDue(ctx context.Context) {
	settings, err := j.settings.Get(ctx)
	if err != nil {
		j.logger.Warn("db metadata objects: load settings failed", "err", err)
		return
	}
	if !settings.DBMetadataObjectEnabled {
		return
	}

	schedule, err := parseCronSchedule(settings.DBMetadataObjectCron)
	if err != nil {
		j.logger.Warn("db metadata objects: invalid cron", "cron", settings.DBMetadataObjectCron, "err", err)
		return
	}
	location, err := time.LoadLocation(strings.TrimSpace(settings.DBMetadataCronTimezone))
	if err != nil {
		j.logger.Warn("db metadata objects: invalid timezone", "timezone", settings.DBMetadataCronTimezone, "err", err)
		return
	}
	now := time.Now().In(location)
	if !schedule.matches(now) {
		return
	}
	scheduledAt := scheduledMinute(now).UTC()
	state, err := j.snapshots.GetJobRun(ctx, objectJobName)
	if err != nil {
		j.logger.Warn("db metadata objects: load job state failed", "err", err)
		return
	}
	if state != nil && state.LastScheduledAt != nil && state.LastScheduledAt.Equal(scheduledAt) {
		return
	}

	j.mu.Lock()
	if j.isRunning {
		j.mu.Unlock()
		return
	}
	j.isRunning = true
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.isRunning = false
		j.mu.Unlock()
	}()

	if err := j.snapshots.MarkJobStarted(ctx, objectJobName, scheduledAt); err != nil {
		j.logger.Warn("db metadata objects: mark start failed", "err", err)
		return
	}
	if err := j.RunOnce(ctx, settings); err != nil {
		_ = j.snapshots.MarkJobFinished(ctx, objectJobName, false, err.Error())
		j.logger.Warn("db metadata objects: run failed", "err", err)
		return
	}
	if err := j.snapshots.MarkJobFinished(ctx, objectJobName, true, ""); err != nil {
		j.logger.Warn("db metadata objects: mark finish failed", "err", err)
	}
}

func (j *DBMetadataObjectJob) RunOnce(ctx context.Context, settings *model.PlatformSettings) error {
	connectionIDs := normalizeObjectConnectionIDs(settings.DBMetadataObjectEnabledConnectionIDs)
	if len(connectionIDs) == 0 {
		if err := j.clearObjectSnapshotsExceptConnectionIDs(ctx, nil); err != nil {
			return fmt.Errorf("clear object snapshots for empty scope: %w", err)
		}
		j.logger.Info("db metadata objects: skip empty scope")
		return nil
	}

	connections, err := j.dbConns.List(ctx)
	if err != nil {
		return fmt.Errorf("list db connections: %w", err)
	}

	selectedConnections := filterObjectConnections(connections, connectionIDs)
	if len(selectedConnections) == 0 {
		if err := j.clearObjectSnapshotsExceptConnectionIDs(ctx, nil); err != nil {
			return fmt.Errorf("clear object snapshots for missing connections: %w", err)
		}
		j.logger.Info("db metadata objects: no matching db connections", "selected_connection_ids", len(connectionIDs))
		return nil
	}

	activeConnectionIDs := make([]uint64, 0, len(selectedConnections))
	for i := range selectedConnections {
		if !isObjectMetadataSupported(selectedConnections[i].DBType) {
			continue
		}
		activeConnectionIDs = append(activeConnectionIDs, selectedConnections[i].ID)
	}
	if err := j.clearObjectSnapshotsExceptConnectionIDs(ctx, activeConnectionIDs); err != nil {
		return fmt.Errorf("clear stale object snapshots: %w", err)
	}

	sem := make(chan struct{}, objectJobConnectionWorkers)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for i := range selectedConnections {
		conn := selectedConnections[i]
		if !isObjectMetadataSupported(conn.DBType) {
			j.logger.Info("db metadata objects: skip unsupported db type", "connection_id", conn.ID, "db_type", conn.DBType)
			continue
		}

		wg.Add(1)
		go func(conn model.DBConnection) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			if err := j.syncConnection(ctx, &conn); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				if clearErr := j.clearObjectSnapshotsForConnection(ctx, conn.ID); clearErr != nil {
					j.logger.Warn("db metadata objects: clear stale snapshots failed", "connection_id", conn.ID, "connection_name", conn.Name, "err", clearErr)
				}
				j.logger.Warn("db metadata objects: connection sync failed", "connection_id", conn.ID, "connection_name", conn.Name, "err", err)
				return
			}
		}(conn)
	}

	wg.Wait()
	return firstErr
}

func (j *DBMetadataObjectJob) clearObjectSnapshotsExceptConnectionIDs(ctx context.Context, connectionIDs []uint64) error {
	if j.snapshots == nil {
		return nil
	}
	return j.snapshots.DeleteObjectSnapshotsExceptConnectionIDs(ctx, connectionIDs)
}

func (j *DBMetadataObjectJob) clearObjectSnapshotsForConnection(ctx context.Context, connectionID uint64) error {
	if j.snapshots == nil {
		return nil
	}
	return j.snapshots.DeleteObjectSnapshotsForConnection(ctx, connectionID)
}

func (j *DBMetadataObjectJob) syncConnection(ctx context.Context, conn *model.DBConnection) error {
	resolvedConn, password, err := j.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		return fmt.Errorf("resolve readonly credential for connection %d: %w", conn.ID, err)
	}

	clusterName, nodeName, err := j.resolveInventoryNames(ctx, resolvedConn)
	if err != nil {
		return err
	}

	snapshotAt := time.Now().UTC()
	var items []model.DBObjectSnapshot
	switch normalizedDBType(resolvedConn.DBType) {
	case "postgres":
		items, err = j.collectPostgresObjects(ctx, resolvedConn, password, snapshotAt, clusterName, nodeName)
	default:
		items, err = j.collectMySQLObjects(ctx, resolvedConn, password, snapshotAt, clusterName, nodeName)
	}
	if err != nil {
		return err
	}

	if err := j.snapshots.ReplaceObjectSnapshotsForConnection(ctx, snapshotAt, resolvedConn.ID, items); err != nil {
		return fmt.Errorf("replace object snapshots for connection %d: %w", resolvedConn.ID, err)
	}

	j.logger.Info("db metadata objects: connection synced", "connection_id", resolvedConn.ID, "connection_name", resolvedConn.Name, "count", len(items))
	return nil
}

func (j *DBMetadataObjectJob) collectMySQLObjects(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	snapshotAt time.Time,
	clusterName *string,
	nodeName *string,
) ([]model.DBObjectSnapshot, error) {
	driver, dsn := pool.BuildDSN(conn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileMetadata)
	if err != nil {
		return nil, fmt.Errorf("open mysql metadata connection %d: %w", conn.ID, err)
	}
	defer db.Close()

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	rows, err := db.QueryContext(queryCtx, `SELECT
		TABLE_SCHEMA,
		TABLE_NAME,
		IFNULL(DATA_LENGTH, 0) AS DATA_LENGTH,
		IFNULL(INDEX_LENGTH, 0) AS INDEX_LENGTH
	FROM information_schema.TABLES
	WHERE TABLE_TYPE = 'BASE TABLE'
	  AND TABLE_SCHEMA NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
	ORDER BY TABLE_SCHEMA, TABLE_NAME`)
	if err != nil {
		return nil, fmt.Errorf("query mysql objects for connection %d: %w", conn.ID, err)
	}
	defer rows.Close()

	items := make([]model.DBObjectSnapshot, 0)
	for rows.Next() {
		var databaseName string
		var tableName string
		var dataSize int64
		var indexSize int64
		if err := rows.Scan(&databaseName, &tableName, &dataSize, &indexSize); err != nil {
			return nil, fmt.Errorf("scan mysql object row for connection %d: %w", conn.ID, err)
		}
		items = append(items, model.DBObjectSnapshot{
			SnapshotAt:     snapshotAt,
			DBConnectionID: conn.ID,
			ConnectionName: conn.Name,
			Engine:         "mysql",
			ClusterName:    clusterName,
			NodeName:       nodeName,
			DatabaseName:   databaseName,
			SchemaName:     databaseName,
			TableName:      tableName,
			DataSizeBytes:  dataSize,
			IndexSizeBytes: indexSize,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql object rows for connection %d: %w", conn.ID, err)
	}
	return items, nil
}

func (j *DBMetadataObjectJob) collectPostgresObjects(
	ctx context.Context,
	conn *model.DBConnection,
	password string,
	snapshotAt time.Time,
	clusterName *string,
	nodeName *string,
) ([]model.DBObjectSnapshot, error) {
	driver, baseDSN := pool.BuildDSN(conn, password)
	baseDB, err := pool.Open(driver, baseDSN, pool.ProfileMetadata)
	if err != nil {
		return nil, fmt.Errorf("open postgres metadata base connection %d: %w", conn.ID, err)
	}
	defer baseDB.Close()

	dbListCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dbRows, err := baseDB.QueryContext(dbListCtx, `SELECT datname
	FROM pg_database
	WHERE datistemplate = false
	  AND datallowconn = true
	ORDER BY datname`)
	if err != nil {
		return nil, fmt.Errorf("query postgres databases for connection %d: %w", conn.ID, err)
	}
	defer dbRows.Close()

	databaseNames := make([]string, 0)
	for dbRows.Next() {
		var databaseName string
		if err := dbRows.Scan(&databaseName); err != nil {
			return nil, fmt.Errorf("scan postgres database for connection %d: %w", conn.ID, err)
		}
		if shouldSkipPostgresMetadataDatabase(databaseName) {
			continue
		}
		databaseNames = append(databaseNames, databaseName)
	}
	if err := dbRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres databases for connection %d: %w", conn.ID, err)
	}

	items := make([]model.DBObjectSnapshot, 0)
	for _, databaseName := range databaseNames {
		dbName := databaseName
		targetDSN := pool.BuildPostgresDSN(conn.Host, conn.Port, conn.Username, password, &dbName, conn.SSLMode)
		targetDB, err := pool.Open("pgx", targetDSN, pool.ProfileMetadata)
		if err != nil {
			return nil, fmt.Errorf("open postgres database %s for connection %d: %w", databaseName, conn.ID, err)
		}

		queryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		rows, err := targetDB.QueryContext(queryCtx, `SELECT
			schemaname,
			relname,
			pg_relation_size(format('%I.%I', schemaname, relname)::regclass) AS data_size_bytes,
			pg_indexes_size(format('%I.%I', schemaname, relname)::regclass) AS index_size_bytes
		FROM pg_stat_user_tables
		ORDER BY schemaname, relname`)
		if err != nil {
			cancel()
			_ = targetDB.Close()
			return nil, fmt.Errorf("query postgres objects for connection %d database %s: %w", conn.ID, databaseName, err)
		}

		for rows.Next() {
			var schemaName string
			var tableName string
			var dataSize int64
			var indexSize int64
			if err := rows.Scan(&schemaName, &tableName, &dataSize, &indexSize); err != nil {
				rows.Close()
				cancel()
				_ = targetDB.Close()
				return nil, fmt.Errorf("scan postgres object row for connection %d database %s: %w", conn.ID, databaseName, err)
			}
			items = append(items, model.DBObjectSnapshot{
				SnapshotAt:     snapshotAt,
				DBConnectionID: conn.ID,
				ConnectionName: conn.Name,
				Engine:         "postgres",
				ClusterName:    clusterName,
				NodeName:       nodeName,
				DatabaseName:   databaseName,
				SchemaName:     schemaName,
				TableName:      tableName,
				DataSizeBytes:  dataSize,
				IndexSizeBytes: indexSize,
			})
		}

		if err := rows.Err(); err != nil {
			rows.Close()
			cancel()
			_ = targetDB.Close()
			return nil, fmt.Errorf("iterate postgres object rows for connection %d database %s: %w", conn.ID, databaseName, err)
		}

		rows.Close()
		cancel()
		if err := targetDB.Close(); err != nil {
			return nil, fmt.Errorf("close postgres database %s for connection %d: %w", databaseName, conn.ID, err)
		}
	}

	return items, nil
}

func (j *DBMetadataObjectJob) resolveInventoryNames(ctx context.Context, conn *model.DBConnection) (*string, *string, error) {
	inventory, err := j.snapshots.FindLatestInventoryByEndpoint(ctx, conn.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve inventory mapping for connection %d: %w", conn.ID, err)
	}
	if inventory == nil {
		return nil, stringPtr(conn.Host), nil
	}

	clusterName := inventory.ClusterIdentifier
	if clusterName == nil || strings.TrimSpace(*clusterName) == "" {
		clusterName = stringPtr(inventory.DBIdentifier)
	}

	nodeName := inventory.InstanceIdentifier
	if nodeName == nil || strings.TrimSpace(*nodeName) == "" {
		nodeName = inventory.InstanceEndpoint
	}
	if nodeName == nil || strings.TrimSpace(*nodeName) == "" {
		nodeName = stringPtr(conn.Host)
	}

	return clusterName, nodeName, nil
}

func normalizeObjectConnectionIDs(ids []uint64) []uint64 {
	out := make([]uint64, 0, len(ids))
	seen := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func filterObjectConnections(connections []model.DBConnection, ids []uint64) []model.DBConnection {
	if len(ids) == 0 {
		return nil
	}
	allowed := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}

	out := make([]model.DBConnection, 0, len(ids))
	for _, conn := range connections {
		if _, ok := allowed[conn.ID]; ok {
			out = append(out, conn)
		}
	}
	return out
}

func isObjectMetadataSupported(dbType string) bool {
	switch normalizedDBType(dbType) {
	case "mysql", "postgres":
		return true
	default:
		return false
	}
}

func normalizedDBType(dbType string) string {
	switch strings.TrimSpace(strings.ToLower(dbType)) {
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return strings.TrimSpace(strings.ToLower(dbType))
	}
}
