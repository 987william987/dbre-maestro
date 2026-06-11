package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/jmoiron/sqlx"
)

const MaxSavedQueriesPerUser = 10

type QueryArtifactRepo struct {
	db *sqlx.DB
}

func NewQueryArtifactRepo(db *sqlx.DB) *QueryArtifactRepo {
	return &QueryArtifactRepo{db: db}
}

func (r *QueryArtifactRepo) AddHistory(ctx context.Context, entry *model.QueryHistoryEntry) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO query_history
		 (user_id, db_connection_id, db_connection_name, database_name, schema_name, redis_db_index, sql_content, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.UserID, entry.DBConnectionID, entry.DBConnectionName, entry.DatabaseName, entry.SchemaName, entry.RedisDBIndex, entry.SQLContent, entry.DurationMs,
	)
	if err != nil {
		return 0, fmt.Errorf("create query_history: %w", err)
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (r *QueryArtifactRepo) ListHistory(ctx context.Context, userID uint64, limit int) ([]model.QueryHistoryEntry, error) {
	history := make([]model.QueryHistoryEntry, 0, limit)
	if err := r.db.SelectContext(ctx, &history,
		`SELECT id, user_id, db_connection_id, db_connection_name, database_name, schema_name, redis_db_index, sql_content, duration_ms, created_at
		 FROM query_history
		 WHERE user_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		userID, limit,
	); err != nil {
		return nil, fmt.Errorf("list query_history: %w", err)
	}
	return history, nil
}

func (r *QueryArtifactRepo) CountSavedQueries(ctx context.Context, userID uint64) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_queries WHERE user_id = ?`, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count saved_queries: %w", err)
	}
	return count, nil
}

func (r *QueryArtifactRepo) CreateSavedQuery(ctx context.Context, query *model.SavedQuery) (uint64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO saved_queries
		 (user_id, label, db_connection_id, db_connection_name, database_name, schema_name, redis_db_index, sql_content)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		query.UserID, query.Label, query.DBConnectionID, query.DBConnectionName, query.DatabaseName, query.SchemaName, query.RedisDBIndex, query.SQLContent,
	)
	if err != nil {
		return 0, fmt.Errorf("create saved_query: %w", err)
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (r *QueryArtifactRepo) ListSavedQueries(ctx context.Context, userID uint64) ([]model.SavedQuery, error) {
	savedQueries := make([]model.SavedQuery, 0, MaxSavedQueriesPerUser)
	if err := r.db.SelectContext(ctx, &savedQueries,
		`SELECT id, user_id, label, db_connection_id, db_connection_name, database_name, schema_name, redis_db_index, sql_content, created_at, updated_at
		 FROM saved_queries
		 WHERE user_id = ?
		 ORDER BY updated_at DESC, id DESC`,
		userID,
	); err != nil {
		return nil, fmt.Errorf("list saved_queries: %w", err)
	}
	return savedQueries, nil
}

func (r *QueryArtifactRepo) DeleteSavedQuery(ctx context.Context, userID, id uint64) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM saved_queries WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete saved_query: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("saved_query rows affected: %w", err)
	}
	return rows > 0, nil
}

func (r *QueryArtifactRepo) FindSavedQueryBySignature(ctx context.Context, userID, dbConnectionID uint64, sqlContent string, databaseName, schemaName *string, redisDBIndex *int) (*model.SavedQuery, error) {
	var query model.SavedQuery
	err := r.db.GetContext(ctx, &query,
		`SELECT id, user_id, label, db_connection_id, db_connection_name, database_name, schema_name, redis_db_index, sql_content, created_at, updated_at
		 FROM saved_queries
		 WHERE user_id = ? AND db_connection_id = ? AND sql_content = ?
		   AND ((database_name IS NULL AND ? IS NULL) OR database_name = ?)
		   AND ((schema_name IS NULL AND ? IS NULL) OR schema_name = ?)
		   AND ((redis_db_index IS NULL AND ? IS NULL) OR redis_db_index = ?)`,
		userID, dbConnectionID, sqlContent,
		databaseName, databaseName,
		schemaName, schemaName,
		redisDBIndex, redisDBIndex,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find saved_query: %w", err)
	}
	return &query, nil
}
