package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/golang-jwt/jwt/v5"
)

const queryContextTokenTTL = 30 * time.Minute

type queryContextScopeClaim struct {
	DatabaseName string `json:"database_name,omitempty"`
	SchemaName   string `json:"schema_name,omitempty"`
	TableName    string `json:"table_name,omitempty"`
	ColumnName   string `json:"column_name"`
}

type queryContextClaims struct {
	UserID            uint64                   `json:"uid"`
	DBConnectionID    uint64                   `json:"db_connection_id"`
	SQLHash           string                   `json:"sql_hash"`
	DatabaseName      string                   `json:"database_name,omitempty"`
	SchemaName        string                   `json:"schema_name,omitempty"`
	ContainsSensitive bool                     `json:"contains_sensitive"`
	Scopes            []queryContextScopeClaim `json:"scopes,omitempty"`
	jwt.RegisteredClaims
}

func newQueryContextToken(secret []byte, userID, connectionID uint64, sqlContent, databaseName, schemaName string, analysis *sqlScopeAnalysis) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("query context signing secret is not configured")
	}
	if analysis == nil {
		analysis = &sqlScopeAnalysis{}
	}

	now := time.Now()
	claims := queryContextClaims{
		UserID:            userID,
		DBConnectionID:    connectionID,
		SQLHash:           hashQueryContextSQL(sqlContent),
		DatabaseName:      strings.TrimSpace(databaseName),
		SchemaName:        strings.TrimSpace(schemaName),
		ContainsSensitive: analysis.ContainsSensitive,
		Scopes:            queryContextScopeClaimsFromScopes(analysis.Scopes),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(queryContextTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Audience:  []string{"query-context"},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func validateQueryContextToken(secret []byte, tokenStr string, userID, connectionID uint64, sqlContent, databaseName, schemaName string) (*sqlScopeAnalysis, error) {
	if strings.TrimSpace(tokenStr) == "" {
		return nil, errors.New("run this SQL successfully before creating the request")
	}
	if len(secret) == 0 {
		return nil, errors.New("query context signing secret is not configured")
	}

	token, err := jwt.ParseWithClaims(strings.TrimSpace(tokenStr), &queryContextClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithAudience("query-context"))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*queryContextClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid query context token")
	}
	if claims.UserID != userID || claims.DBConnectionID != connectionID {
		return nil, errors.New("query context does not match this request")
	}
	if claims.SQLHash != hashQueryContextSQL(sqlContent) {
		return nil, errors.New("SQL changed after execution; run the query again before creating the request")
	}
	if claims.DatabaseName != strings.TrimSpace(databaseName) || claims.SchemaName != strings.TrimSpace(schemaName) {
		return nil, errors.New("database context changed after execution; run the query again before creating the request")
	}

	return &sqlScopeAnalysis{
		ContainsSensitive: claims.ContainsSensitive,
		Scopes:            scopesFromQueryContextClaims(connectionID, claims.Scopes),
	}, nil
}

func hashQueryContextSQL(sqlContent string) string {
	sum := sha256.Sum256([]byte(sqlContent))
	return hex.EncodeToString(sum[:])
}

func queryContextScopeClaimsFromScopes(scopes []model.TicketScope) []queryContextScopeClaim {
	claims := make([]queryContextScopeClaim, 0, len(scopes))
	for _, scope := range scopes {
		claims = append(claims, queryContextScopeClaim{
			DatabaseName: nullableStringValue(scope.DatabaseName),
			SchemaName:   nullableStringValue(scope.SchemaName),
			TableName:    nullableStringValue(scope.TableName),
			ColumnName:   strings.TrimSpace(scope.ColumnName),
		})
	}
	return claims
}

func scopesFromQueryContextClaims(connectionID uint64, claims []queryContextScopeClaim) []model.TicketScope {
	scopes := make([]model.TicketScope, 0, len(claims))
	for _, claim := range claims {
		columnName := strings.TrimSpace(claim.ColumnName)
		if columnName == "" {
			continue
		}
		scopes = append(scopes, model.TicketScope{
			ConnectionID: connectionID,
			DatabaseName: optionalTrimmedString(claim.DatabaseName),
			SchemaName:   optionalTrimmedString(claim.SchemaName),
			TableName:    optionalTrimmedString(claim.TableName),
			ColumnName:   columnName,
			IsSensitive:  true,
			SourceKind:   "query_column",
		})
	}
	return scopes
}
