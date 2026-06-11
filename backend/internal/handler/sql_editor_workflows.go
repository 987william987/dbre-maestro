package handler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type sqlScopeAnalysis struct {
	Scopes          []model.TicketScope
	ContainsSensitive bool
}

func analyzeSQLScopes(
	ctx context.Context,
	dbConns *repository.DBConnectionRepo,
	maskingRuntime *maskingRuntime,
	conn *model.DBConnection,
	sqlContent string,
	queryCtx queryExecutionContext,
) (*sqlScopeAnalysis, error) {
	password, err := dbConns.DecryptPassword(conn)
	if err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}

	driver, dsn := pool.BuildDSN(conn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("get query pool: %w", err)
	}

	execSQL := injectLimit(sqlContent, 200, conn.DBType)
	result, err := executeQueryForConnection(ctx, conn, password, pools.QueryPool, execSQL, queryCtx)
	if err != nil {
		return nil, err
	}

	_, sensitiveIndexes, err := maskingRuntime.applyResult(ctx, conn, 0, result)
	if err != nil {
		return nil, err
	}
	sensitiveIndexSet := make(map[int]bool, len(sensitiveIndexes))
	for _, idx := range sensitiveIndexes {
		sensitiveIndexSet[idx] = true
	}

	scopes := make([]model.TicketScope, 0, len(result.Origins))
	seen := make(map[string]struct{}, len(result.Origins))
	for idx, origin := range result.Origins {
		columnName := strings.TrimSpace(origin.Column)
		if columnName == "" {
			columnName = strings.TrimSpace(result.Columns[idx])
		}
		if columnName == "" {
			continue
		}

		databaseName := optionalTrimmedString(origin.Database)
		schemaName := optionalTrimmedString(origin.Schema)
		tableName := optionalTrimmedString(origin.Table)
		key := fmt.Sprintf("%d|%s|%s|%s|%s|%t",
			conn.ID,
			nullableStringValue(databaseName),
			nullableStringValue(schemaName),
			nullableStringValue(tableName),
			columnName,
			sensitiveIndexSet[idx],
		)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		scopes = append(scopes, model.TicketScope{
			ConnectionID: conn.ID,
			DatabaseName: databaseName,
			SchemaName:   schemaName,
			TableName:    tableName,
			ColumnName:   columnName,
			IsSensitive:  sensitiveIndexSet[idx],
			SourceKind:   "query_column",
		})
	}

	return &sqlScopeAnalysis{
		Scopes:            scopes,
		ContainsSensitive: len(sensitiveIndexes) > 0,
	}, nil
}

func nullableStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func buildQueryExecutionContext(databaseName, schemaName string) queryExecutionContext {
	return queryExecutionContext{
		DatabaseName: strings.TrimSpace(databaseName),
		SchemaName:   strings.TrimSpace(schemaName),
	}
}

func userCanAccessConnection(ctx context.Context, users *repository.UserRepo, userID, connectionID uint64) (bool, error) {
	accessibleIDs, err := users.GetEffectiveDBConnectionIDs(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, accessibleID := range accessibleIDs {
		if accessibleID == connectionID {
			return true, nil
		}
	}
	return false, nil
}

func openConnectionForTicket(ctx context.Context, dbConns *repository.DBConnectionRepo, connectionID uint64) (*model.DBConnection, error) {
	conn, err := dbConns.GetByID(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, sql.ErrNoRows
	}
	return conn, nil
}
