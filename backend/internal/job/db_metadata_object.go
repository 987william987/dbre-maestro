package job

import (
	"context"
	"database/sql"
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
	lastRunAt time.Time
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

	j.runIfDue(ctx)
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

	intervalMinutes := settings.DBMetadataObjectSyncIntervalMins
	if intervalMinutes <= 0 {
		intervalMinutes = 60
	}

	j.mu.Lock()
	if j.isRunning {
		j.mu.Unlock()
		return
	}
	if !j.lastRunAt.IsZero() && time.Since(j.lastRunAt) < time.Duration(intervalMinutes)*time.Minute {
		j.mu.Unlock()
		return
	}
	j.isRunning = true
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.isRunning = false
		j.lastRunAt = time.Now()
		j.mu.Unlock()
	}()

	if err := j.RunOnce(ctx, settings); err != nil {
		j.logger.Warn("db metadata objects: run failed", "err", err)
		return
	}
}

func (j *DBMetadataObjectJob) RunOnce(ctx context.Context, settings *model.PlatformSettings) error {
	connectionIDs := normalizeObjectConnectionIDs(settings.DBMetadataObjectEnabledConnectionIDs)
	if len(connectionIDs) == 0 {
		j.logger.Info("db metadata objects: skip empty scope")
		return nil
	}

	connections, err := j.dbConns.List(ctx)
	if err != nil {
		return fmt.Errorf("list db connections: %w", err)
	}

	selectedConnections := filterObjectConnections(connections, connectionIDs)
	if len(selectedConnections) == 0 {
		j.logger.Info("db metadata objects: no matching db connections", "selected_connection_ids", len(connectionIDs))
		return nil
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
				j.logger.Warn("db metadata objects: connection sync failed", "connection_id", conn.ID, "connection_name", conn.Name, "err", err)
				return
			}
		}(conn)
	}

	wg.Wait()
	return firstErr
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
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql metadata connection %d: %w", conn.ID, err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(2 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)

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
	baseDB, err := sql.Open(driver, baseDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres metadata base connection %d: %w", conn.ID, err)
	}
	defer baseDB.Close()

	baseDB.SetMaxOpenConns(1)
	baseDB.SetMaxIdleConns(1)
	baseDB.SetConnMaxLifetime(2 * time.Minute)
	baseDB.SetConnMaxIdleTime(time.Minute)

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
		targetDB, err := sql.Open("pgx", targetDSN)
		if err != nil {
			return nil, fmt.Errorf("open postgres database %s for connection %d: %w", databaseName, conn.ID, err)
		}

		targetDB.SetMaxOpenConns(1)
		targetDB.SetMaxIdleConns(1)
		targetDB.SetConnMaxLifetime(2 * time.Minute)
		targetDB.SetConnMaxIdleTime(time.Minute)

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
