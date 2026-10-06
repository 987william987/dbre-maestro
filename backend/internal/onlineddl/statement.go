package onlineddl

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
	tidbast "github.com/pingcap/tidb/pkg/parser/ast"
	tidbformat "github.com/pingcap/tidb/pkg/parser/format"
)

type Statement struct {
	Database    string `json:"database"`
	Table       string `json:"table"`
	AlterClause string `json:"alter_clause"`
	SQLSHA256   string `json:"sql_sha256"`
}

func ParseApprovedAlter(sqlText, selectedDatabase string) (Statement, error) {
	parsed, err := sqlparse.ParseSQL(sqlparse.DialectMySQL, sqlText)
	if err != nil || len(parsed.Statements) != 1 {
		return Statement{}, ErrUnsupportedStatement
	}
	alter, ok := parsed.Statements[0].AST.(*tidbast.AlterTableStmt)
	if !ok || alter.Table == nil || len(alter.Specs) == 0 {
		return Statement{}, ErrUnsupportedStatement
	}
	database := alter.Table.Schema.O
	if database == "" {
		database = strings.TrimSpace(selectedDatabase)
	}
	table := alter.Table.Name.O
	if database == "" || table == "" {
		return Statement{}, ErrUnsupportedStatement
	}
	var clause strings.Builder
	ctx := tidbformat.NewRestoreCtx(tidbformat.DefaultRestoreFlags, &clause)
	for i, spec := range alter.Specs {
		if spec == nil {
			return Statement{}, ErrUnsupportedStatement
		}
		if i > 0 {
			clause.WriteString(", ")
		}
		if err := spec.Restore(ctx); err != nil {
			return Statement{}, ErrUnsupportedStatement
		}
	}
	sum := sha256.Sum256([]byte(sqlText))
	return Statement{Database: database, Table: table, AlterClause: clause.String(), SQLSHA256: hex.EncodeToString(sum[:])}, nil
}
