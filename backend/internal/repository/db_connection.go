package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/netguard"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type DBConnectionRepo struct {
	db         *sqlx.DB
	encKey     []byte
	hostPolicy *netguard.Policy
}

func NewDBConnectionRepo(db *sqlx.DB, encKey []byte, options ...DBConnectionRepoOption) *DBConnectionRepo {
	r := &DBConnectionRepo{db: db, encKey: encKey}
	for _, option := range options {
		option(r)
	}
	return r
}

type DBConnectionRepoOption func(*DBConnectionRepo)

func WithDBConnectionHostPolicy(policy *netguard.Policy) DBConnectionRepoOption {
	return func(r *DBConnectionRepo) {
		r.hostPolicy = policy
	}
}

func (r *DBConnectionRepo) Create(ctx context.Context, c *model.DBConnection, plainPassword string, credentials []model.DBConnectionCredentialInput) (*model.DBConnection, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create db_connection tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	legacyEnc, err := crypto.Encrypt(r.encKey, []byte(plainPassword))
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO db_connections
         (name, db_type, host, port, readonly_host, readonly_port, readwrite_host, readwrite_port, database_name, username, password_encrypted, encryption_key_version, ssl_mode, created_by, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
		c.Name, c.DBType, c.Host, c.Port, c.ReadonlyHost, c.ReadonlyPort, c.ReadwriteHost, c.ReadwritePort, c.DatabaseName,
		c.Username, legacyEnc, c.SSLMode, c.CreatedBy, timeutil.NowUTC(), timeutil.NowUTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("create db_connection: %w", err)
	}
	id, _ := res.LastInsertId()

	if err := r.replaceCredentialsTx(ctx, tx, uint64(id), credentials); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create db_connection tx: %w", err)
	}
	tx = nil
	return r.GetByID(ctx, uint64(id))
}

func (r *DBConnectionRepo) GetByID(ctx context.Context, id uint64) (*model.DBConnection, error) {
	var c model.DBConnection
	err := r.db.GetContext(ctx, &c, `SELECT * FROM db_connections WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.loadCredentials(ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *DBConnectionRepo) List(ctx context.Context) ([]model.DBConnection, error) {
	var conns []model.DBConnection
	err := r.db.SelectContext(ctx, &conns, `SELECT * FROM db_connections ORDER BY name`)
	if err != nil {
		return nil, err
	}
	if err := r.loadCredentialsForMany(ctx, conns); err != nil {
		return nil, err
	}
	return conns, nil
}

func (r *DBConnectionRepo) Delete(ctx context.Context, id uint64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete db_connection tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := cleanupDBConnectionReferences(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM db_connections WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete db_connection: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete db_connection tx: %w", err)
	}
	tx = nil
	return nil
}

func cleanupDBConnectionReferences(ctx context.Context, tx *sqlx.Tx, connectionID uint64) error {
	if err := removeDBConnectionFromObjectScanSettings(ctx, tx, connectionID); err != nil {
		return err
	}

	now := timeutil.NowUTC()
	statements := []struct {
		query string
		args  []any
		label string
	}{
		{query: `DELETE FROM db_connection_credentials WHERE db_connection_id = ?`, args: []any{connectionID}, label: "db connection credentials"},
		{query: `DELETE FROM user_db_connections WHERE db_connection_id = ?`, args: []any{connectionID}, label: "user db connection grants"},
		{query: `DELETE FROM auth_group_db_connections WHERE db_connection_id = ?`, args: []any{connectionID}, label: "auth group db connection grants"},
		{query: `DELETE FROM db_object_snapshots WHERE db_connection_id = ?`, args: []any{connectionID}, label: "db object snapshots"},
		{query: `DELETE FROM ticket_execution_rollbacks WHERE source_connection_id = ?`, args: []any{connectionID}, label: "ticket execution rollbacks"},
		{query: `DELETE FROM masking_whitelist WHERE db_connection_id = ?`, args: []any{connectionID}, label: "masking whitelist"},
		{query: `DELETE FROM masking_rules WHERE db_connection_id = ?`, args: []any{connectionID}, label: "masking rules"},
		{query: `DELETE FROM redis_sensitive_key_prefixes WHERE db_connection_id = ?`, args: []any{connectionID}, label: "redis sensitive key prefixes"},
		{query: `DELETE FROM query_access_rules WHERE connection_id = ?`, args: []any{connectionID}, label: "query access rules"},
		{query: `DELETE FROM query_access_grants WHERE connection_id = ?`, args: []any{connectionID}, label: "legacy query access grants"},
		{query: `DELETE FROM scheduled_sql_report_runs WHERE report_id IN (SELECT id FROM scheduled_sql_reports WHERE db_connection_id = ?)`, args: []any{connectionID}, label: "scheduled sql report runs"},
		{query: `DELETE FROM scheduled_sql_reports WHERE db_connection_id = ?`, args: []any{connectionID}, label: "scheduled sql reports"},
		{query: `UPDATE workflow_rules SET db_connection_id = NULL, updated_at = ? WHERE db_connection_id = ?`, args: []any{now, connectionID}, label: "workflow rules"},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("cleanup %s for db_connection %d: %w", statement.label, connectionID, err)
		}
	}
	return nil
}

func removeDBConnectionFromObjectScanSettings(ctx context.Context, tx *sqlx.Tx, connectionID uint64) error {
	var raw string
	err := tx.GetContext(ctx, &raw, `SELECT value FROM platform_settings WHERE key_name = ?`, settingDBMetadataObjectConnectionIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load object scan connection setting: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var items []uint64
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return fmt.Errorf("decode object scan connection setting: %w", err)
	}
	next := make([]uint64, 0, len(items))
	removed := false
	for _, item := range items {
		if item == connectionID {
			removed = true
			continue
		}
		next = append(next, item)
	}
	if !removed {
		return nil
	}
	if err := upsertUint64List(ctx, tx, settingDBMetadataObjectConnectionIDs, next); err != nil {
		return fmt.Errorf("update object scan connection setting: %w", err)
	}
	return nil
}

// DecryptPassword decrypts the stored password for use when opening a connection pool.
func (r *DBConnectionRepo) DecryptPassword(c *model.DBConnection) (string, error) {
	plain, err := crypto.Decrypt(r.encKey, c.PasswordEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt password for connection %d: %w", c.ID, err)
	}
	return string(plain), nil
}

func (r *DBConnectionRepo) ResolveCredential(conn *model.DBConnection, role string) (*model.DBConnection, string, error) {
	targetRole := strings.TrimSpace(role)
	if targetRole == "" {
		targetRole = model.DBCredentialRoleReadonly
	}

	for _, credential := range conn.Credentials {
		if credential.CredentialRole != targetRole {
			continue
		}
		plain, err := crypto.Decrypt(r.encKey, credential.PasswordEncrypted)
		if err != nil {
			return nil, "", fmt.Errorf("decrypt %s credential for connection %d: %w", targetRole, conn.ID, err)
		}
		cloned := *conn
		cloned.Username = credential.Username
		applyConnectionEndpoint(&cloned, targetRole)
		if err := r.checkResolvedEndpoint(context.Background(), &cloned, targetRole); err != nil {
			return nil, "", err
		}
		return &cloned, string(plain), nil
	}

	if len(conn.PasswordEncrypted) == 0 {
		return nil, "", fmt.Errorf("credential role %s not configured for connection %d", targetRole, conn.ID)
	}

	plain, err := crypto.Decrypt(r.encKey, conn.PasswordEncrypted)
	if err != nil {
		return nil, "", fmt.Errorf("decrypt legacy credential for connection %d: %w", conn.ID, err)
	}
	cloned := *conn
	applyConnectionEndpoint(&cloned, targetRole)
	if err := r.checkResolvedEndpoint(context.Background(), &cloned, targetRole); err != nil {
		return nil, "", err
	}
	return &cloned, string(plain), nil
}

func (r *DBConnectionRepo) checkResolvedEndpoint(ctx context.Context, conn *model.DBConnection, role string) error {
	if r == nil || r.hostPolicy == nil || !r.hostPolicy.Enabled() || conn == nil {
		return nil
	}
	report, err := r.hostPolicy.Check(ctx, role, conn.Host, conn.Port)
	if len(report.Violations) > 0 {
		slog.Warn("db connection host policy violation",
			"connection_id", conn.ID,
			"connection_name", conn.Name,
			"role", role,
			"host", report.Host,
			"port", report.Port,
			"ips", report.IPs,
			"violations", report.Violations,
			"enforcement", report.Enforcement,
		)
	}
	return err
}

func (r *DBConnectionRepo) UpdatePassword(ctx context.Context, id uint64, plainPassword string) error {
	enc, err := crypto.Encrypt(r.encKey, []byte(plainPassword))
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE db_connections SET password_encrypted = ?, encryption_key_version = 1, updated_at = ? WHERE id = ?`,
		enc, timeutil.NowUTC(), id,
	)
	return err
}

// Update patches non-sensitive fields. Call UpdatePassword separately if password changed.
func (r *DBConnectionRepo) Update(ctx context.Context, id uint64, name, dbType, host string, port uint16, readonlyHost string, readonlyPort uint16, readwriteHost string, readwritePort uint16, databaseName *string, username, sslMode string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE db_connections
         SET name=?, db_type=?, host=?, port=?, readonly_host=?, readonly_port=?, readwrite_host=?, readwrite_port=?, database_name=?, username=?, ssl_mode=?, updated_at=?
         WHERE id=?`,
		name, dbType, host, port, readonlyHost, readonlyPort, readwriteHost, readwritePort, databaseName, username, sslMode, timeutil.NowUTC(), id,
	)
	return err
}

func (r *DBConnectionRepo) ReplaceCredentials(ctx context.Context, id uint64, credentials []model.DBConnectionCredentialInput) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace credentials tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := r.replaceCredentialsTx(ctx, tx, id, credentials); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace credentials tx: %w", err)
	}
	tx = nil
	return nil
}

func (r *DBConnectionRepo) RecordTestResult(ctx context.Context, id uint64, ok bool, message string) (time.Time, error) {
	status := "passed"
	var errMsg any
	if !ok {
		status = "failed"
		errMsg = strings.TrimSpace(message)
	}
	testedAt := timeutil.NowUTC()
	_, err := r.db.ExecContext(ctx,
		`UPDATE db_connections
		 SET last_test_status = ?, last_test_error = ?, last_tested_at = ?, updated_at = ?
		 WHERE id = ?`,
		status, errMsg, testedAt, testedAt, id,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("record db_connection test result: %w", err)
	}
	return testedAt, nil
}

func (r *DBConnectionRepo) replaceCredentialsTx(ctx context.Context, tx *sqlx.Tx, dbConnectionID uint64, credentials []model.DBConnectionCredentialInput) error {
	var existing []model.DBConnectionCredential
	if err := tx.SelectContext(ctx, &existing,
		`SELECT * FROM db_connection_credentials WHERE db_connection_id = ?`, dbConnectionID); err != nil {
		return fmt.Errorf("load existing db_connection_credentials: %w", err)
	}
	byRole := make(map[string]model.DBConnectionCredential, len(existing))
	for _, item := range existing {
		byRole[item.CredentialRole] = item
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM db_connection_credentials WHERE db_connection_id = ?`, dbConnectionID); err != nil {
		return fmt.Errorf("clear db_connection_credentials: %w", err)
	}

	for _, credential := range credentials {
		if strings.TrimSpace(credential.CredentialRole) == "" {
			continue
		}
		if strings.TrimSpace(credential.Username) == "" {
			continue
		}
		enc := []byte(nil)
		if credential.Password == "" {
			if current, ok := byRole[credential.CredentialRole]; ok && current.Username == credential.Username {
				enc = current.PasswordEncrypted
			}
		} else {
			var err error
			enc, err = crypto.Encrypt(r.encKey, []byte(credential.Password))
			if err != nil {
				return fmt.Errorf("encrypt %s credential: %w", credential.CredentialRole, err)
			}
		}
		if len(enc) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO db_connection_credentials
			 (db_connection_id, credential_role, username, password_encrypted, encryption_key_version, created_at)
			 VALUES (?, ?, ?, ?, 1, ?)`,
			dbConnectionID, credential.CredentialRole, credential.Username, enc, timeutil.NowUTC(),
		); err != nil {
			return fmt.Errorf("insert %s credential: %w", credential.CredentialRole, err)
		}
	}
	return nil
}

func (r *DBConnectionRepo) loadCredentials(ctx context.Context, conn *model.DBConnection) error {
	var credentials []model.DBConnectionCredential
	if err := r.db.SelectContext(ctx, &credentials,
		`SELECT * FROM db_connection_credentials WHERE db_connection_id = ? ORDER BY credential_role`, conn.ID); err != nil {
		return fmt.Errorf("load db_connection_credentials for %d: %w", conn.ID, err)
	}
	for i := range credentials {
		credentials[i].HasPassword = len(credentials[i].PasswordEncrypted) > 0
	}
	conn.Credentials = credentials
	return nil
}

func (r *DBConnectionRepo) loadCredentialsForMany(ctx context.Context, conns []model.DBConnection) error {
	if len(conns) == 0 {
		return nil
	}

	query, args, err := sqlx.In(`SELECT * FROM db_connection_credentials WHERE db_connection_id IN (?) ORDER BY db_connection_id, credential_role`, collectConnectionIDs(conns))
	if err != nil {
		return fmt.Errorf("build db_connection_credentials query: %w", err)
	}
	query = r.db.Rebind(query)

	var credentials []model.DBConnectionCredential
	if err := r.db.SelectContext(ctx, &credentials, query, args...); err != nil {
		return fmt.Errorf("load db_connection_credentials: %w", err)
	}

	byConnectionID := make(map[uint64][]model.DBConnectionCredential, len(conns))
	for _, credential := range credentials {
		credential.HasPassword = len(credential.PasswordEncrypted) > 0
		byConnectionID[credential.DBConnectionID] = append(byConnectionID[credential.DBConnectionID], credential)
	}
	for i := range conns {
		conns[i].Credentials = byConnectionID[conns[i].ID]
	}
	return nil
}

func collectConnectionIDs(conns []model.DBConnection) []uint64 {
	ids := make([]uint64, 0, len(conns))
	for _, conn := range conns {
		ids = append(ids, conn.ID)
	}
	return ids
}

func applyConnectionEndpoint(conn *model.DBConnection, role string) {
	switch strings.TrimSpace(role) {
	case model.DBCredentialRoleReadwrite, model.DBCredentialRoleRollback:
		conn.Host = conn.EffectiveReadwriteHost()
		conn.Port = conn.EffectiveReadwritePort()
	default:
		conn.Host = conn.EffectiveReadonlyHost()
		conn.Port = conn.EffectiveReadonlyPort()
	}
}
