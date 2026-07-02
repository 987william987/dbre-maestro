package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/timeutil"
	"github.com/jmoiron/sqlx"
)

type RedisSensitiveKeyPrefixRepo struct {
	db *sqlx.DB
}

func NewRedisSensitiveKeyPrefixRepo(db *sqlx.DB) *RedisSensitiveKeyPrefixRepo {
	return &RedisSensitiveKeyPrefixRepo{db: db}
}

func (r *RedisSensitiveKeyPrefixRepo) List(ctx context.Context) ([]model.RedisSensitiveKeyPrefix, error) {
	var prefixes []model.RedisSensitiveKeyPrefix
	err := r.db.SelectContext(ctx, &prefixes,
		`SELECT id, db_connection_id, redis_db_index, key_prefix, reason, is_active, created_by, created_at, updated_at
		 FROM redis_sensitive_key_prefixes
		 ORDER BY db_connection_id, redis_db_index IS NULL DESC, redis_db_index, key_prefix`,
	)
	return prefixes, err
}

func (r *RedisSensitiveKeyPrefixRepo) ListActiveForConnection(ctx context.Context, connID uint64, dbIndex int) ([]model.RedisSensitiveKeyPrefix, error) {
	var prefixes []model.RedisSensitiveKeyPrefix
	err := r.db.SelectContext(ctx, &prefixes,
		`SELECT id, db_connection_id, redis_db_index, key_prefix, reason, is_active, created_by, created_at, updated_at
		 FROM redis_sensitive_key_prefixes
		 WHERE db_connection_id = ?
		   AND is_active = 1
		   AND (redis_db_index IS NULL OR redis_db_index = ?)
		 ORDER BY redis_db_index IS NULL DESC, key_prefix`,
		connID, dbIndex,
	)
	return prefixes, err
}

func (r *RedisSensitiveKeyPrefixRepo) Create(ctx context.Context, prefix *model.RedisSensitiveKeyPrefix) (*model.RedisSensitiveKeyPrefix, error) {
	exists, err := r.Exists(ctx, prefix.DBConnectionID, prefix.RedisDBIndex, prefix.KeyPrefix, 0)
	if err != nil {
		return nil, fmt.Errorf("check redis sensitive key prefix exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("redis sensitive key prefix already exists")
	}

	res, err := r.db.ExecContext(ctx,
		`INSERT INTO redis_sensitive_key_prefixes (db_connection_id, redis_db_index, key_prefix, reason, is_active, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		prefix.DBConnectionID, prefix.RedisDBIndex, strings.TrimSpace(prefix.KeyPrefix), nullableRedisReason(prefix.Reason), prefix.IsActive, prefix.CreatedBy, timeutil.NowUTC(), timeutil.NowUTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("create redis sensitive key prefix: %w", err)
	}
	id, _ := res.LastInsertId()
	return r.GetByID(ctx, uint64(id))
}

func (r *RedisSensitiveKeyPrefixRepo) GetByID(ctx context.Context, id uint64) (*model.RedisSensitiveKeyPrefix, error) {
	var prefix model.RedisSensitiveKeyPrefix
	err := r.db.GetContext(ctx, &prefix,
		`SELECT id, db_connection_id, redis_db_index, key_prefix, reason, is_active, created_by, created_at, updated_at
		 FROM redis_sensitive_key_prefixes
		 WHERE id = ?`,
		id,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &prefix, err
}

func (r *RedisSensitiveKeyPrefixRepo) Patch(ctx context.Context, prefix *model.RedisSensitiveKeyPrefix) (*model.RedisSensitiveKeyPrefix, error) {
	exists, err := r.Exists(ctx, prefix.DBConnectionID, prefix.RedisDBIndex, prefix.KeyPrefix, prefix.ID)
	if err != nil {
		return nil, fmt.Errorf("check redis sensitive key prefix exists: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("redis sensitive key prefix already exists")
	}

	_, err = r.db.ExecContext(ctx,
		`UPDATE redis_sensitive_key_prefixes
		 SET db_connection_id = ?, redis_db_index = ?, key_prefix = ?, reason = ?, is_active = ?
		 WHERE id = ?`,
		prefix.DBConnectionID, prefix.RedisDBIndex, strings.TrimSpace(prefix.KeyPrefix), nullableRedisReason(prefix.Reason), prefix.IsActive, prefix.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("patch redis sensitive key prefix: %w", err)
	}
	return r.GetByID(ctx, prefix.ID)
}

func (r *RedisSensitiveKeyPrefixRepo) Delete(ctx context.Context, id uint64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM redis_sensitive_key_prefixes WHERE id = ?`, id)
	return err
}

func (r *RedisSensitiveKeyPrefixRepo) Exists(ctx context.Context, connID uint64, dbIndex *int, keyPrefix string, excludeID uint64) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM redis_sensitive_key_prefixes
		WHERE db_connection_id = ?
		  AND LOWER(key_prefix) = LOWER(?)`
	args := []any{connID, strings.TrimSpace(keyPrefix)}
	if dbIndex == nil {
		query += ` AND redis_db_index IS NULL`
	} else {
		query += ` AND redis_db_index = ?`
		args = append(args, *dbIndex)
	}
	if excludeID != 0 {
		query += ` AND id <> ?`
		args = append(args, excludeID)
	}
	if err := r.db.GetContext(ctx, &count, query, args...); err != nil {
		return false, err
	}
	return count > 0, nil
}

func RedisSensitiveKeyPrefixValues(prefixes []model.RedisSensitiveKeyPrefix) []string {
	values := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		value := strings.TrimSpace(prefix.KeyPrefix)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func nullableRedisReason(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
