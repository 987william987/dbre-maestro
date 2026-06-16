package sqlpolicy

import (
	"fmt"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type ErrReadOnlyStatementCount struct {
	Count int
}

func (e *ErrReadOnlyStatementCount) Error() string {
	return fmt.Sprintf("SQL Editor only supports a single statement, got %d statements", e.Count)
}

type ErrReadOnlyViolation struct {
	Statement string
	Kind      sqlparse.StatementKind
}

func (e *ErrReadOnlyViolation) Error() string {
	return fmt.Sprintf("statement type %q is not allowed in SQL Editor", e.Kind)
}

func CheckReadOnlySingleStatement(statements []sqlparse.ParsedStatement) error {
	if len(statements) != 1 {
		return &ErrReadOnlyStatementCount{Count: len(statements)}
	}

	kind := statements[0].Kind
	switch kind {
	case sqlparse.StatementKindSelect, sqlparse.StatementKindShow, sqlparse.StatementKindExplain, sqlparse.StatementKindDescribe:
		return nil
	default:
		return &ErrReadOnlyViolation{
			Statement: statements[0].RawSQL,
			Kind:      kind,
		}
	}
}
