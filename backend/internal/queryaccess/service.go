package queryaccess

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type ObjectRef struct {
	ConnectionID uint64
	DatabaseName string
	SchemaName   string
	TableName    string
}

type CheckContext struct {
	DatabaseName string
	SchemaName   string
}

type MissingAccessError struct {
	Missing []ObjectRef
}

func (e *MissingAccessError) Error() string {
	if len(e.Missing) == 0 {
		return "query access is missing"
	}
	first := e.Missing[0]
	if first.TableName != "" {
		return fmt.Sprintf("You do not have query access to %s.%s", first.DatabaseName, first.TableName)
	}
	return fmt.Sprintf("You do not have query access to %s", first.DatabaseName)
}

type Service struct {
	repo  *repository.QueryAccessRepo
	users *repository.UserRepo
}

func NewService(repo *repository.QueryAccessRepo, users *repository.UserRepo) *Service {
	return &Service{repo: repo, users: users}
}

func (s *Service) CheckSQL(ctx context.Context, userID uint64, conn *model.DBConnection, sqlText string, checkCtx CheckContext) error {
	if s == nil || s.repo == nil || conn == nil {
		return nil
	}
	if s.users != nil {
		hasAllPermissions, err := s.users.HasAllPermissions(ctx, userID)
		if err != nil {
			return err
		}
		if hasAllPermissions {
			return nil
		}
	}
	refs, err := ExtractObjectRefs(conn, sqlText, checkCtx)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}

	authGroupIDs := []uint64{}
	if s.users != nil {
		authGroupIDs, err = s.users.GetEffectiveAuthGroupIDs(ctx, userID)
		if err != nil {
			return err
		}
	}
	rules, err := s.repo.ListActiveRules(ctx, userID, authGroupIDs, conn.ID)
	if err != nil {
		return err
	}
	missing := make([]ObjectRef, 0)
	for _, ref := range refs {
		if !isAllowedByRules(ref, rules) {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		return &MissingAccessError{Missing: dedupeRefs(missing)}
	}
	return nil
}

func (s *Service) CheckRedis(ctx context.Context, userID uint64, conn *model.DBConnection, dbIndex int, command string, args []string) error {
	if s == nil || s.repo == nil || conn == nil {
		return nil
	}
	if s.users != nil {
		hasAllPermissions, err := s.users.HasAllPermissions(ctx, userID)
		if err != nil {
			return err
		}
		if hasAllPermissions {
			return nil
		}
	}

	authGroupIDs := []uint64{}
	if s.users != nil {
		var err error
		authGroupIDs, err = s.users.GetEffectiveAuthGroupIDs(ctx, userID)
		if err != nil {
			return err
		}
	}
	rules, err := s.repo.ListActiveRules(ctx, userID, authGroupIDs, conn.ID)
	if err != nil {
		return err
	}

	ref := ObjectRef{
		ConnectionID: conn.ID,
		DatabaseName: strconv.Itoa(dbIndex),
		TableName:    redisQueryAccessKey(command, args),
	}
	if !isAllowedByRules(ref, rules) {
		return &MissingAccessError{Missing: []ObjectRef{ref}}
	}
	return nil
}

func isAllowedByRules(ref ObjectRef, rules []model.QueryAccessRule) bool {
	allowed := false
	for _, rule := range rules {
		if !matchesRule(ref, rule) {
			continue
		}
		if rule.Effect == model.QueryAccessEffectDeny {
			return false
		}
		allowed = true
	}
	return allowed
}

func matchesRule(ref ObjectRef, rule model.QueryAccessRule) bool {
	if rule.ConnectionID != ref.ConnectionID {
		return false
	}
	if !matchesPattern(rule.DatabasePattern, ref.DatabaseName) {
		return false
	}
	return matchesPattern(rule.TablePattern, ref.TableName)
}

func matchesPattern(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "*" {
		return true
	}
	return equalFold(pattern, value)
}

func redisQueryAccessKey(command string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(command)) {
	case "GET", "GETRANGE", "HGET", "HMGET", "HSCAN", "LINDEX", "SISMEMBER", "SMISMEMBER", "SSCAN", "SRANDMEMBER", "ZSCORE", "ZMSCORE", "ZRANK", "ZCOUNT", "ZSCAN":
		return args[0]
	case "MGET":
		if len(args) == 1 {
			return args[0]
		}
		return ""
	default:
		return ""
	}
}

func matchesAnyGrant(ref ObjectRef, grants []model.QueryAccessGrant) bool {
	for _, grant := range grants {
		if grant.ConnectionID != ref.ConnectionID {
			continue
		}
		if !equalFold(nullableString(grant.DatabaseName), ref.DatabaseName) {
			continue
		}
		if grant.TableName == nil || strings.TrimSpace(*grant.TableName) == "" {
			return true
		}
		if equalFold(*grant.TableName, ref.TableName) {
			return true
		}
	}
	return false
}

func ExtractObjectRefs(conn *model.DBConnection, sqlText string, checkCtx CheckContext) ([]ObjectRef, error) {
	dialect := sqlparse.DialectFromDBType(conn.DBType)
	parsed, err := sqlparse.ParseSQL(dialect, sqlText)
	if err != nil {
		return nil, err
	}
	if len(parsed.Statements) == 0 {
		return nil, nil
	}
	refs := make([]ObjectRef, 0)
	for _, stmt := range parsed.Statements {
		stmtRefs, err := extractStatementRefs(dialect, conn, stmt, checkCtx)
		if err != nil {
			return nil, err
		}
		refs = append(refs, stmtRefs...)
	}
	return dedupeRefs(refs), nil
}

func dedupeRefs(items []ObjectRef) []ObjectRef {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]ObjectRef, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.DatabaseName) == "" || strings.TrimSpace(item.TableName) == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(item.DatabaseName)) + "|" +
			strings.ToLower(strings.TrimSpace(item.SchemaName)) + "|" +
			strings.ToLower(strings.TrimSpace(item.TableName))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func equalFold(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
