package handler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/pool"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type sqlScopeAnalysis struct {
	Scopes            []model.TicketScope
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
	resolvedConn, password, err := dbConns.ResolveCredential(conn, model.DBCredentialRoleReadonly)
	if err != nil {
		return nil, fmt.Errorf("decrypt password: %w", err)
	}

	driver, dsn := pool.BuildDSN(resolvedConn, password)
	pools, err := pool.Global().GetOrCreate(conn.ID, driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("get query pool: %w", err)
	}

	execSQL := injectLimit(sqlContent, 200, conn.DBType)
	result, err := executeQueryForConnection(ctx, resolvedConn, password, pools.QueryPool, execSQL, queryCtx, defaultSQLEditorTimeoutSettings())
	if err != nil {
		return nil, err
	}

	decisions, _, err := maskingRuntime.analyzeSensitiveColumns(ctx, resolvedConn, result)
	if err != nil {
		return nil, err
	}
	return buildSQLScopeAnalysisFromDecisions(conn.ID, decisions), nil
}

func buildSQLScopeAnalysisFromDecisions(connectionID uint64, decisions []sensitiveColumnDecision) *sqlScopeAnalysis {
	scopeOrigins := make([]masking.ColumnOrigin, 0, len(decisions))
	for _, decision := range decisions {
		scopeOrigins = append(scopeOrigins, decision.SensitiveOrigins...)
	}

	scopes := make([]model.TicketScope, 0, len(scopeOrigins))
	seen := make(map[string]struct{}, len(scopeOrigins))
	for _, origin := range scopeOrigins {
		columnName := strings.TrimSpace(origin.Column)
		if columnName == "" {
			continue
		}

		databaseName := optionalTrimmedString(origin.Database)
		schemaName := optionalTrimmedString(origin.Schema)
		tableName := optionalTrimmedString(origin.Table)
		key := fmt.Sprintf("%d|%s|%s|%s|%s|%t",
			connectionID,
			nullableStringValue(databaseName),
			nullableStringValue(schemaName),
			nullableStringValue(tableName),
			columnName,
			true,
		)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		scopes = append(scopes, model.TicketScope{
			ConnectionID: connectionID,
			DatabaseName: databaseName,
			SchemaName:   schemaName,
			TableName:    tableName,
			ColumnName:   columnName,
			IsSensitive:  true,
			SourceKind:   "query_column",
		})
	}

	return &sqlScopeAnalysis{
		Scopes:            scopes,
		ContainsSensitive: len(decisions) > 0,
	}
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

func listAccessibleConnections(ctx context.Context, dbConns *repository.DBConnectionRepo, users *repository.UserRepo, userID uint64) ([]model.DBConnection, error) {
	accessibleIDs, err := users.GetEffectiveDBConnectionIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	connections, err := dbConns.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(connections) == 0 {
		return []model.DBConnection{}, nil
	}

	accessibleIDSet := make(map[uint64]struct{}, len(accessibleIDs))
	for _, connectionID := range accessibleIDs {
		accessibleIDSet[connectionID] = struct{}{}
	}

	filtered := make([]model.DBConnection, 0, len(connections))
	for _, connection := range connections {
		if _, ok := accessibleIDSet[connection.ID]; ok {
			filtered = append(filtered, connection)
		}
	}
	return filtered, nil
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
