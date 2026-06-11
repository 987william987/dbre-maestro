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
	engine    *masking.Engine
}

func newMaskingRuntime(
	users *repository.UserRepo,
	rules *repository.MaskingRuleRepo,
	whitelist *repository.MaskingWhitelistRepo,
	engine *masking.Engine,
) *maskingRuntime {
	return &maskingRuntime{
		users:     users,
		rules:     rules,
		whitelist: whitelist,
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

func (m *maskingRuntime) applyResult(ctx context.Context, conn *model.DBConnection, userID uint64, result *masking.QueryResult) (bool, error) {
	if !supportsMySQLMasking(conn) || result == nil || m.engine == nil {
		return false, nil
	}

	override, err := m.hasSensitiveOverride(ctx, userID)
	if err != nil {
		return false, err
	}
	if override {
		return true, nil
	}

	dbRules, err := m.rules.ListForConnection(ctx, conn.ID)
	if err != nil {
		return false, err
	}
	if len(dbRules) == 0 {
		return false, nil
	}

	preciseRules := make([]masking.Rule, 0, len(result.Columns))
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
				return false, err
			}
			if exempt {
				continue
			}
		}

		preciseRules = append(preciseRules, masking.Rule{
			Database: databaseName,
			Table:    tableName,
			Column:   actualColumnName,
			Mode:     masking.MaskMode(mode),
		})
	}

	if len(preciseRules) == 0 {
		return false, nil
	}
	if err := m.engine.MaskResult(result, preciseRules); err != nil {
		return false, err
	}
	return false, nil
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
