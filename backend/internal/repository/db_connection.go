package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

func (r *DBConnectionRepo) Create(ctx context.Context, c *model.DBConnection, plainPassword string) (*model.DBConnection, error) {
	enc, err := crypto.Encrypt(r.encKey, []byte(plainPassword))
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO db_connections
         (name, db_type, host, port, database_name, username, password_encrypted, encryption_key_version, ssl_mode, created_by)
         VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		c.Name, c.DBType, c.Host, c.Port, c.DatabaseName,
		c.Username, enc, c.SSLMode, c.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("create db_connection: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *DBConnectionRepo) GetByID(ctx context.Context, id uint64) (*model.DBConnection, error) {
	var c model.DBConnection
	err := r.db.GetContext(ctx, &c, `SELECT * FROM db_connections WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &c, err
}

func (r *DBConnectionRepo) List(ctx context.Context) ([]model.DBConnection, error) {
	var conns []model.DBConnection
	err := r.db.SelectContext(ctx, &conns, `SELECT * FROM db_connections ORDER BY name`)
	return conns, err
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
