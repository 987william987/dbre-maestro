package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/crypto"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

type DBConnectionRepo struct {
	db     *sqlx.DB
	encKey []byte
}

func NewDBConnectionRepo(db *sqlx.DB, encKey []byte) *DBConnectionRepo {
	return &DBConnectionRepo{db: db, encKey: encKey}
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
         (name, db_type, host, port, database_name, username, password_encrypted, encryption_key_version, ssl_mode, created_by)
         VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.Name, c.DBType, c.Host, c.Port, c.DatabaseName,
		c.Username, legacyEnc, c.SSLMode, c.CreatedBy,
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
	_, err := r.db.ExecContext(ctx, `DELETE FROM db_connections WHERE id = ?`, id)
	return err
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
	return &cloned, string(plain), nil
}

func (r *DBConnectionRepo) UpdatePassword(ctx context.Context, id uint64, plainPassword string) error {
	enc, err := crypto.Encrypt(r.encKey, []byte(plainPassword))
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE db_connections SET password_encrypted = ?, encryption_key_version = 1, updated_at = NOW() WHERE id = ?`,
		enc, id,
	)
	return err
}

// Update patches non-sensitive fields. Call UpdatePassword separately if password changed.
func (r *DBConnectionRepo) Update(ctx context.Context, id uint64, name, dbType, host string, port uint16, databaseName *string, username, sslMode string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE db_connections
         SET name=?, db_type=?, host=?, port=?, database_name=?, username=?, ssl_mode=?, updated_at=NOW()
         WHERE id=?`,
		name, dbType, host, port, databaseName, username, sslMode, id,
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
			 (db_connection_id, credential_role, username, password_encrypted, encryption_key_version)
			 VALUES (?, ?, ?, ?, 1)`,
			dbConnectionID, credential.CredentialRole, credential.Username, enc,
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
