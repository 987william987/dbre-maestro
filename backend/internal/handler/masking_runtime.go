package handler

import (
	"context"
	"strings"

	"github.com/dbre-maestro/maestro/internal/masking"
	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type maskingRuntime struct {
	users     *repository.UserRepo
	rules     *repository.MaskingRuleRepo
	whitelist *repository.MaskingWhitelistRepo
	tickets   *repository.TicketRepo
	engine    *masking.Engine
}

func newMaskingRuntime(
	users *repository.UserRepo,
	rules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	tickets *repository.TicketRepo,
	engine *masking.Engine,
) *maskingRuntime {
	return &maskingRuntime{
		users:     users,
		rules:     rules,
		whitelist: whitelist,
		tickets:   tickets,
		engine:    engine,
	}
}

func (m *maskingRuntime) isSensitiveConnection(ctx context.Context, conn *model.DBConnection) (bool, error) {
	if !supportsMySQLMasking(conn) {
		return false, nil
	}
	rules, err := m.rules.ListForConnection(ctx, conn.ID)
	if err != nil {
		return false, err
	}
	return len(rules) > 0, nil
}

func (m *maskingRuntime) hasSensitiveOverride(ctx context.Context, userID uint64) (bool, error) {
	if userID == 0 || m.users == nil {
		return false, nil
	}
	permissions, err := m.users.GetEffectivePermissionKeys(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, permission := range permissions {
		if permission == "global.sensitive" {
			return true, nil
		}
	}
	return false, nil
}

func (m *maskingRuntime) applyResult(ctx context.Context, conn *model.DBConnection, userID uint64, result *masking.QueryResult) (bool, []int, error) {
	preciseRules, sensitiveIndexes, err := m.analyzeSensitiveColumns(ctx, conn, result)
	if err != nil {
		return false, nil, err
	}
	if len(preciseRules) == 0 || m.engine == nil {
		return false, nil, nil
	}

	override, err := m.hasSensitiveOverride(ctx, userID)
	if err != nil {
		return false, nil, err
	}
	if override {
		return true, sensitiveIndexes, nil
	}

	grantedIndexes, err := m.activeSensitiveAccessIndexes(ctx, conn, userID, result, sensitiveIndexes)
	if err != nil {
		return false, nil, err
	}
	filteredRules := make([]masking.Rule, 0, len(preciseRules))
	for idx, rule := range preciseRules {
		columnIndex := sensitiveIndexes[idx]
		if grantedIndexes[columnIndex] {
			continue
		}
		filteredRules = append(filteredRules, rule)
	}
	if len(filteredRules) == 0 {
		return false, sensitiveIndexes, nil
	}
	if err := m.engine.MaskResult(result, filteredRules); err != nil {
		return false, nil, err
	}
	return false, sensitiveIndexes, nil
}

func (m *maskingRuntime) analyzeSensitiveColumns(ctx context.Context, conn *model.DBConnection, result *masking.QueryResult) ([]masking.Rule, []int, error) {
	if !supportsMySQLMasking(conn) || result == nil {
		return nil, nil, nil
	}

	dbRules, err := m.rules.ListForConnection(ctx, conn.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(dbRules) == 0 {
		return nil, nil, nil
	}

	preciseRules := make([]masking.Rule, 0, len(result.Columns))
	sensitiveIndexes := make([]int, 0, len(result.Columns))
	for idx, columnLabel := range result.Columns {
		origin := masking.ColumnOrigin{}
		if idx < len(result.Origins) {
			origin = result.Origins[idx]
		}
		actualColumnName := originColumnName(origin, columnLabel)
		if actualColumnName == "" {
			continue
		}

		mode, ok := findMaskMode(dbRules, actualColumnName)
		if !ok {
			continue
		}

		databaseName := originDatabaseName(conn, origin)
		tableName := originTableName(origin, columnLabel)
		if tableName != "" && databaseName != "" && m.whitelist != nil {
			exempt, err := m.whitelist.Match(ctx, conn.ID, databaseName, tableName, actualColumnName)
			if err != nil {
				return nil, nil, err
			}
			if exempt {
				continue
			}
		}

		sensitiveIndexes = append(sensitiveIndexes, idx)
		preciseRules = append(preciseRules, masking.Rule{
			Database: databaseName,
			Table:    tableName,
			Column:   actualColumnName,
			Mode:     masking.MaskMode(mode),
		})
	}
	return preciseRules, sensitiveIndexes, nil
}

func (m *maskingRuntime) activeSensitiveAccessIndexes(
	ctx context.Context,
	conn *model.DBConnection,
	userID uint64,
	result *masking.QueryResult,
	sensitiveIndexes []int,
) (map[int]bool, error) {
	grantedIndexes := make(map[int]bool)
	if userID == 0 || m.tickets == nil || len(sensitiveIndexes) == 0 || conn == nil {
		return grantedIndexes, nil
	}

	scopes, err := m.tickets.ListActiveSensitiveAccessScopes(ctx, userID, conn.ID)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return grantedIndexes, nil
	}

	for _, idx := range sensitiveIndexes {
		if idx >= len(result.Origins) {
			continue
		}
		origin := result.Origins[idx]
		for _, scope := range scopes {
			if scopeMatchesOrigin(scope, origin) {
				grantedIndexes[idx] = true
				break
			}
		}
	}
	return grantedIndexes, nil
}

func supportsMySQLMasking(conn *model.DBConnection) bool {
	return conn != nil && conn.DBType == "mysql"
}

func findMaskMode(rules []model.MaskingRule, columnName string) (string, bool) {
	for _, rule := range rules {
		if strings.EqualFold(strings.TrimSpace(rule.ColumnName), strings.TrimSpace(columnName)) {
			return rule.MaskMode, true
		}
	}
	return "", false
}

func originColumnName(origin masking.ColumnOrigin, columnLabel string) string {
	if strings.TrimSpace(origin.Column) != "" {
		return strings.TrimSpace(origin.Column)
	}
	parts := strings.Split(strings.TrimSpace(columnLabel), ".")
	return strings.TrimSpace(parts[len(parts)-1])
}

func originTableName(origin masking.ColumnOrigin, columnLabel string) string {
	if strings.TrimSpace(origin.Table) != "" {
		return strings.TrimSpace(origin.Table)
	}
	parts := strings.Split(strings.TrimSpace(columnLabel), ".")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[len(parts)-2])
	}
	return ""
}

func originDatabaseName(conn *model.DBConnection, origin masking.ColumnOrigin) string {
	if strings.TrimSpace(origin.Database) != "" {
		return strings.TrimSpace(origin.Database)
	}
	return connectionDatabaseName(conn)
}

func scopeMatchesOrigin(scope model.TicketScope, origin masking.ColumnOrigin) bool {
	if !strings.EqualFold(strings.TrimSpace(scope.ColumnName), strings.TrimSpace(origin.Column)) {
		return false
	}
	if scope.TableName != nil && strings.TrimSpace(*scope.TableName) != "" && !strings.EqualFold(strings.TrimSpace(*scope.TableName), strings.TrimSpace(origin.Table)) {
		return false
	}
	if scope.SchemaName != nil && strings.TrimSpace(*scope.SchemaName) != "" && !strings.EqualFold(strings.TrimSpace(*scope.SchemaName), strings.TrimSpace(origin.Schema)) {
		return false
	}
	if scope.DatabaseName != nil && strings.TrimSpace(*scope.DatabaseName) != "" && !strings.EqualFold(strings.TrimSpace(*scope.DatabaseName), strings.TrimSpace(origin.Database)) {
		return false
	}
	return true
}
