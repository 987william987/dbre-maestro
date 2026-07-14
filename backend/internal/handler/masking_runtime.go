package handler

import (
	"context"
	"fmt"
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

type sensitiveColumnDecision struct {
	ColumnIndex      int
	Rule             masking.Rule
	SensitiveOrigins []masking.ColumnOrigin
}

type matchedMaskRule struct {
	Origin masking.ColumnOrigin
	Rule   model.MaskingRule
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
	if !supportsSQLMasking(conn) {
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
	decisions, sensitiveIndexes, err := m.analyzeSensitiveColumns(ctx, conn, result)
	if err != nil {
		return false, nil, err
	}
	return m.applyAnalyzedResult(ctx, conn, userID, result, decisions, sensitiveIndexes)
}

func (m *maskingRuntime) applyAnalyzedResult(
	ctx context.Context,
	conn *model.DBConnection,
	userID uint64,
	result *masking.QueryResult,
	decisions []sensitiveColumnDecision,
	sensitiveIndexes []int,
) (bool, []int, error) {
	if len(decisions) == 0 {
		return false, nil, nil
	}
	if m.engine == nil {
		return false, nil, fmt.Errorf("masking engine is not configured")
	}

	override, err := m.hasSensitiveOverride(ctx, userID)
	if err != nil {
		return false, nil, err
	}
	if override {
		return true, sensitiveIndexes, nil
	}

	grantedIndexes, err := m.activeSensitiveAccessIndexes(ctx, conn, userID, decisions)
	if err != nil {
		return false, nil, err
	}
	filteredRules := make(map[int]masking.Rule, len(decisions))
	for _, decision := range decisions {
		if grantedIndexes[decision.ColumnIndex] {
			continue
		}
		filteredRules[decision.ColumnIndex] = decision.Rule
	}
	if len(filteredRules) == 0 {
		return false, sensitiveIndexes, nil
	}
	if err := m.engine.MaskColumns(result, filteredRules); err != nil {
		return false, nil, err
	}
	return false, sensitiveIndexes, nil
}

func (m *maskingRuntime) analyzeSensitiveColumns(ctx context.Context, conn *model.DBConnection, result *masking.QueryResult) ([]sensitiveColumnDecision, []int, error) {
	if !supportsSQLMasking(conn) || result == nil {
		return nil, nil, nil
	}

	dbRules, err := m.rules.ListForConnection(ctx, conn.ID)
	if err != nil {
		return nil, nil, err
	}
	if len(dbRules) == 0 {
		return nil, nil, nil
	}

	decisions := make([]sensitiveColumnDecision, 0, len(result.Columns))
	sensitiveIndexes := make([]int, 0, len(result.Columns))
	for idx, columnLabel := range result.Columns {
		dependencies := resultColumnDependencies(result, idx, conn, columnLabel)
		if len(dependencies) == 0 {
			continue
		}

		sensitiveOrigins := make([]masking.ColumnOrigin, 0, len(dependencies))
		matchedRules := make([]matchedMaskRule, 0, len(dependencies))
		seenOrigins := make(map[string]struct{}, len(dependencies))
		for _, dependency := range dependencies {
			actualColumnName := originColumnName(dependency, columnLabel)
			if actualColumnName == "" {
				continue
			}

			ruleMatch, ok, err := findMaskRule(dbRules, actualColumnName)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				continue
			}

			databaseName := originDatabaseName(conn, dependency)
			tableName := originTableName(dependency, columnLabel)
			if tableName != "" && databaseName != "" && m.whitelist != nil {
				exempt, err := m.whitelist.Match(ctx, conn.ID, databaseName, dependency.Schema, tableName, actualColumnName)
				if err != nil {
					return nil, nil, err
				}
				if exempt {
					continue
				}
			}

			origin := masking.ColumnOrigin{
				Database: databaseName,
				Schema:   dependency.Schema,
				Table:    tableName,
				Column:   actualColumnName,
			}
			key := strings.ToLower(strings.TrimSpace(origin.Database)) + "|" +
				strings.ToLower(strings.TrimSpace(origin.Schema)) + "|" +
				strings.ToLower(strings.TrimSpace(origin.Table)) + "|" +
				strings.ToLower(strings.TrimSpace(origin.Column))
			if _, ok := seenOrigins[key]; ok {
				continue
			}
			seenOrigins[key] = struct{}{}
			sensitiveOrigins = append(sensitiveOrigins, origin)
			matchedRules = append(matchedRules, matchedMaskRule{
				Origin: origin,
				Rule:   ruleMatch,
			})
		}
		finalRule, ok := decideMaskRuleForResultColumn(columnLabel, matchedRules)
		if !ok || len(sensitiveOrigins) == 0 {
			continue
		}

		sensitiveIndexes = append(sensitiveIndexes, idx)
		decisions = append(decisions, sensitiveColumnDecision{
			ColumnIndex:      idx,
			Rule:             finalRule,
			SensitiveOrigins: sensitiveOrigins,
		})
	}
	return decisions, sensitiveIndexes, nil
}

func (m *maskingRuntime) activeSensitiveAccessIndexes(
	ctx context.Context,
	conn *model.DBConnection,
	userID uint64,
	decisions []sensitiveColumnDecision,
) (map[int]bool, error) {
	grantedIndexes := make(map[int]bool)
	if userID == 0 || m.tickets == nil || len(decisions) == 0 || conn == nil {
		return grantedIndexes, nil
	}

	scopes, err := m.tickets.ListActiveSensitiveAccessScopes(ctx, userID, conn.ID)
	if err != nil {
		return nil, err
	}
	if len(scopes) == 0 {
		return grantedIndexes, nil
	}

	for _, decision := range decisions {
		allGranted := true
		for _, origin := range decision.SensitiveOrigins {
			matched := false
			for _, scope := range scopes {
				if scopeMatchesOrigin(scope, origin) {
					matched = true
					break
				}
			}
			if !matched {
				allGranted = false
				break
			}
		}
		if allGranted {
			grantedIndexes[decision.ColumnIndex] = true
		}
	}
	return grantedIndexes, nil
}

func supportsSQLMasking(conn *model.DBConnection) bool {
	if conn == nil {
		return false
	}
	switch conn.DBType {
	case "mysql", "postgres", "postgresql":
		return true
	default:
		return false
	}
}

func decideMaskRuleForResultColumn(columnLabel string, matches []matchedMaskRule) (masking.Rule, bool) {
	if len(matches) == 0 {
		return masking.Rule{}, false
	}

	selected := masking.Rule{
		Column: columnLabel,
		Match:  masking.MatchTypeExact,
		Mode:   masking.MaskMode(matches[0].Rule.MaskMode),
		Config: matches[0].Rule.MaskConfig,
	}

	if len(matches) == 1 {
		return selected, true
	}

	firstMode := masking.MaskMode(matches[0].Rule.MaskMode)
	for _, match := range matches[1:] {
		if masking.MaskMode(match.Rule.MaskMode) != firstMode {
			return masking.Rule{
				Column: columnLabel,
				Match:  masking.MatchTypeExact,
				Mode:   masking.MaskModeFull,
			}, true
		}
	}

	return selected, true
}

func findMaskRule(rules []model.MaskingRule, columnName string) (model.MaskingRule, bool, error) {
	for _, rule := range rules {
		matches, err := masking.MatchColumnPattern(masking.Rule{
			Column: rule.ColumnName,
			Match:  masking.MatchType(rule.MatchType),
		}, columnName)
		if err != nil {
			return model.MaskingRule{}, false, fmt.Errorf("match masking rule %d for column %q: %w", rule.ID, columnName, err)
		}
		if matches {
			return rule, true, nil
		}
	}
	return model.MaskingRule{}, false, nil
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

func resultColumnDependencies(result *masking.QueryResult, idx int, conn *model.DBConnection, columnLabel string) []masking.ColumnOrigin {
	if result != nil && idx < len(result.Dependencies) && len(result.Dependencies[idx]) > 0 {
		return result.Dependencies[idx]
	}

	origin := masking.ColumnOrigin{}
	if result != nil && idx < len(result.Origins) {
		origin = result.Origins[idx]
	}
	actualColumnName := originColumnName(origin, columnLabel)
	if actualColumnName == "" {
		return nil
	}

	return []masking.ColumnOrigin{{
		Database: originDatabaseName(conn, origin),
		Schema:   origin.Schema,
		Table:    originTableName(origin, columnLabel),
		Column:   actualColumnName,
	}}
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
