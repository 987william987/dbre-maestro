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

const accountJobName = "db_metadata_account"

type DBMetadataAccountJob struct {
	settings  *repository.SettingsRepo
	dbConns   *repository.DBConnectionRepo
	snapshots *repository.DBMetadataRepo
	logger    *slog.Logger
	mu        sync.Mutex
	isRunning bool
}

func NewDBMetadataAccountJob(settings *repository.SettingsRepo, dbConns *repository.DBConnectionRepo, snapshots *repository.DBMetadataRepo, logger *slog.Logger) *DBMetadataAccountJob {
	if logger == nil {
		logger = slog.Default()
	}
	return &DBMetadataAccountJob{settings: settings, dbConns: dbConns, snapshots: snapshots, logger: logger}
}

func (j *DBMetadataAccountJob) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
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

func (j *DBMetadataAccountJob) runIfDue(ctx context.Context) {
	settings, err := j.settings.Get(ctx)
	if err != nil || !settings.DBMetadataAccountEnabled {
		return
	}
	schedule, err := parseCronSchedule(settings.DBMetadataAccountCron)
	if err != nil {
		j.logger.Warn("db account metadata: invalid cron", "err", err)
		return
	}
	location, err := time.LoadLocation(strings.TrimSpace(settings.DBMetadataCronTimezone))
	if err != nil {
		j.logger.Warn("db account metadata: invalid timezone", "err", err)
		return
	}
	now := time.Now().In(location)
	if !schedule.matches(now) {
		return
	}
	scheduledAt := scheduledMinute(now).UTC()
	state, err := j.snapshots.GetJobRun(ctx, accountJobName)
	if err != nil || state != nil && state.LastScheduledAt != nil && state.LastScheduledAt.Equal(scheduledAt) {
		return
	}
	j.mu.Lock()
	if j.isRunning {
		j.mu.Unlock()
		return
	}
	j.isRunning = true
	j.mu.Unlock()
	defer func() { j.mu.Lock(); j.isRunning = false; j.mu.Unlock() }()
	_ = j.snapshots.MarkJobStarted(ctx, accountJobName, scheduledAt)
	err = j.RunOnce(ctx, settings)
	message := ""
	if err != nil {
		message = err.Error()
		j.logger.Warn("db account metadata: scan failed", "err", err)
	}
	_ = j.snapshots.MarkJobFinished(ctx, accountJobName, err == nil, message)
}

func (j *DBMetadataAccountJob) RunOnce(ctx context.Context, settings *model.PlatformSettings) error {
	selected := make(map[uint64]struct{}, len(settings.DBMetadataAccountEnabledConnectionIDs))
	for _, id := range settings.DBMetadataAccountEnabledConnectionIDs {
		if id > 0 {
			selected[id] = struct{}{}
		}
	}
	connections, err := j.dbConns.List(ctx)
	if err != nil {
		return err
	}
	activeConnectionIDs := make([]uint64, 0, len(selected))
	for i := range connections {
		if _, ok := selected[connections[i].ID]; ok && (normalizedDBType(connections[i].DBType) == "mysql" || normalizedDBType(connections[i].DBType) == "postgres") {
			activeConnectionIDs = append(activeConnectionIDs, connections[i].ID)
		}
	}
	if err := j.snapshots.DeleteAccountSnapshotsExceptConnectionIDs(ctx, activeConnectionIDs); err != nil {
		return fmt.Errorf("clear stale account snapshots: %w", err)
	}
	var firstErr error
	for i := range connections {
		conn := &connections[i]
		if _, ok := selected[conn.ID]; !ok || normalizedDBType(conn.DBType) != "mysql" && normalizedDBType(conn.DBType) != "postgres" {
			continue
		}
		if err := j.syncConnection(ctx, conn); err != nil {
			_ = j.snapshots.MarkAccountSnapshotFailed(ctx, conn.ID, time.Now().UTC(), err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (j *DBMetadataAccountJob) syncConnection(ctx context.Context, conn *model.DBConnection) error {
	resolved, password, err := j.dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		return err
	}
	snapshotAt := time.Now().UTC()
	var accounts []model.DBAccountSnapshot
	var grants []model.DBAccountGrantSnapshot
	if normalizedDBType(resolved.DBType) == "postgres" {
		accounts, grants, err = collectPostgresAccounts(ctx, resolved, password, snapshotAt)
	} else {
		accounts, grants, err = collectMySQLAccounts(ctx, resolved, password, snapshotAt)
	}
	if err != nil {
		return fmt.Errorf("scan accounts for connection %d: %w", conn.ID, err)
	}
	return j.snapshots.ReplaceAccountSnapshots(ctx, conn.ID, snapshotAt, accounts, grants)
}

func collectMySQLAccounts(ctx context.Context, conn *model.DBConnection, password string, snapshotAt time.Time) ([]model.DBAccountSnapshot, []model.DBAccountGrantSnapshot, error) {
	driver, dsn := pool.BuildDSN(conn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileMetadata)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT CONCAT(QUOTE(User), '@', QUOTE(Host)), User, Host, IF(account_locked = 'Y' AND authentication_string = '', 'role', 'user'), account_locked = 'N', Super_priv = 'Y', account_locked = 'Y', password_expired = 'Y' FROM mysql.user ORDER BY User, Host`)
	if err != nil {
		return nil, nil, err
	}
	accounts := make([]model.DBAccountSnapshot, 0)
	for rows.Next() {
		var item model.DBAccountSnapshot
		var passwordExpired bool
		if err := rows.Scan(&item.PrincipalKey, &item.PrincipalName, &item.PrincipalHost, &item.PrincipalType, &item.CanLogin, &item.IsSuperuser, &item.IsLocked, &passwordExpired); err != nil {
			rows.Close()
			return nil, nil, err
		}
		item.Engine, item.DBConnectionID, item.SnapshotAt = "mysql", conn.ID, snapshotAt
		accounts = append(accounts, item)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	grants := make([]model.DBAccountGrantSnapshot, 0)
	for _, account := range accounts {
		host := "%"
		if account.PrincipalHost != nil {
			host = *account.PrincipalHost
		}
		showGrantRows, err := db.QueryContext(ctx, `SHOW GRANTS FOR `+quoteMySQLAccountPart(account.PrincipalName)+`@`+quoteMySQLAccountPart(host))
		if err != nil {
			return nil, nil, fmt.Errorf("show grants for %s: %w", account.PrincipalKey, err)
		}
		for showGrantRows.Next() {
			var statement string
			if err := showGrantRows.Scan(&statement); err != nil {
				showGrantRows.Close()
				return nil, nil, err
			}
			grants = append(grants, model.DBAccountGrantSnapshot{
				DBConnectionID: conn.ID,
				SnapshotAt:     snapshotAt,
				PrincipalKey:   account.PrincipalKey,
				GrantKind:      "native_statement",
				GrantStatement: &statement,
			})
		}
		if err := showGrantRows.Close(); err != nil {
			return nil, nil, err
		}
	}
	grantRows, err := db.QueryContext(ctx, `SELECT GRANTEE, 'privilege', NULL, 'global', NULL, NULL, NULL, PRIVILEGE_TYPE, IS_GRANTABLE = 'YES' FROM information_schema.USER_PRIVILEGES
	UNION ALL SELECT GRANTEE, 'privilege', NULL, 'database', TABLE_SCHEMA, NULL, NULL, PRIVILEGE_TYPE, IS_GRANTABLE = 'YES' FROM information_schema.SCHEMA_PRIVILEGES
	UNION ALL SELECT GRANTEE, 'privilege', NULL, 'table', TABLE_CATALOG, TABLE_SCHEMA, TABLE_NAME, PRIVILEGE_TYPE, IS_GRANTABLE = 'YES' FROM information_schema.TABLE_PRIVILEGES`)
	if err != nil {
		return nil, nil, err
	}
	for grantRows.Next() {
		var item model.DBAccountGrantSnapshot
		if err := grantRows.Scan(&item.PrincipalKey, &item.GrantKind, &item.GrantedRole, &item.ScopeType, &item.DatabaseName, &item.SchemaName, &item.ObjectName, &item.PrivilegeType, &item.IsGrantable); err != nil {
			grantRows.Close()
			return nil, nil, err
		}
		item.DBConnectionID, item.SnapshotAt = conn.ID, snapshotAt
		grants = append(grants, item)
	}
	if err := grantRows.Close(); err != nil {
		return nil, nil, err
	}
	roleRows, err := db.QueryContext(ctx, `SELECT CONCAT(QUOTE(TO_USER), '@', QUOTE(TO_HOST)), CONCAT(FROM_USER, '@', FROM_HOST), WITH_ADMIN_OPTION = 'Y' FROM mysql.role_edges`)
	if err == nil {
		defer roleRows.Close()
		for roleRows.Next() {
			item := model.DBAccountGrantSnapshot{DBConnectionID: conn.ID, SnapshotAt: snapshotAt, GrantKind: "role_membership"}
			if err := roleRows.Scan(&item.PrincipalKey, &item.GrantedRole, &item.IsGrantable); err != nil {
				return nil, nil, err
			}
			grants = append(grants, item)
		}
	}
	return accounts, grants, nil
}

func quoteMySQLAccountPart(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func collectPostgresAccounts(ctx context.Context, conn *model.DBConnection, password string, snapshotAt time.Time) ([]model.DBAccountSnapshot, []model.DBAccountGrantSnapshot, error) {
	driver, dsn := pool.BuildDSN(conn, password)
	db, err := pool.Open(driver, dsn, pool.ProfileMetadata)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT rolname, rolname, CASE WHEN rolcanlogin THEN 'user' ELSE 'role' END, rolcanlogin, rolsuper, rolinherit, rolcreaterole, rolcreatedb, rolreplication, rolbypassrls, false, CASE WHEN rolvaliduntil::text = 'infinity' THEN NULL ELSE rolvaliduntil END FROM pg_roles WHERE LEFT(rolname, 3) <> 'pg_' ORDER BY rolname`)
	if err != nil {
		return nil, nil, err
	}
	accounts := make([]model.DBAccountSnapshot, 0)
	for rows.Next() {
		var item model.DBAccountSnapshot
		if err := rows.Scan(&item.PrincipalKey, &item.PrincipalName, &item.PrincipalType, &item.CanLogin, &item.IsSuperuser, &item.InheritsRoles, &item.CanCreateRole, &item.CanCreateDB, &item.CanReplicate, &item.CanBypassRLS, &item.IsLocked, &item.ValidUntil); err != nil {
			rows.Close()
			return nil, nil, err
		}
		item.Engine, item.DBConnectionID, item.SnapshotAt = "postgres", conn.ID, snapshotAt
		accounts = append(accounts, item)
	}
	_ = rows.Close()
	grants := make([]model.DBAccountGrantSnapshot, 0)
	roleRows, err := db.QueryContext(ctx, `SELECT member.rolname, role.rolname, m.admin_option FROM pg_auth_members m JOIN pg_roles role ON role.oid = m.roleid JOIN pg_roles member ON member.oid = m.member`)
	if err != nil {
		return nil, nil, err
	}
	for roleRows.Next() {
		item := model.DBAccountGrantSnapshot{DBConnectionID: conn.ID, SnapshotAt: snapshotAt, GrantKind: "role_membership"}
		if err := roleRows.Scan(&item.PrincipalKey, &item.GrantedRole, &item.IsGrantable); err != nil {
			roleRows.Close()
			return nil, nil, err
		}
		grants = append(grants, item)
	}
	_ = roleRows.Close()
	grantRows, err := db.QueryContext(ctx, `SELECT grantee, 'privilege', 'table', table_catalog, table_schema, table_name, privilege_type, is_grantable = 'YES' FROM information_schema.role_table_grants
	UNION ALL SELECT grantee, 'privilege', 'routine', specific_catalog, specific_schema, routine_name, privilege_type, is_grantable = 'YES' FROM information_schema.role_routine_grants`)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	if grantRows != nil {
		defer grantRows.Close()
		for grantRows.Next() {
			item := model.DBAccountGrantSnapshot{DBConnectionID: conn.ID, SnapshotAt: snapshotAt}
			if err := grantRows.Scan(&item.PrincipalKey, &item.GrantKind, &item.ScopeType, &item.DatabaseName, &item.SchemaName, &item.ObjectName, &item.PrivilegeType, &item.IsGrantable); err != nil {
				return nil, nil, err
			}
			grants = append(grants, item)
		}
	}
	return accounts, grants, nil
}
