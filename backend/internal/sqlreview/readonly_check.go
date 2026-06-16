package sqlreview

import (
	"fmt"

	"github.com/dbre-maestro/maestro/internal/sqlparse"
	"github.com/dbre-maestro/maestro/internal/sqlpolicy"
)

type ErrReadOnlyViolation struct {
	Statement string
	Keyword   string
}

func (e *ErrReadOnlyViolation) Error() string {
	preview := e.Statement
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	return fmt.Sprintf("statement type %q is not allowed in SQL Editor (only SELECT/SHOW/EXPLAIN/DESC are permitted): %s", e.Keyword, preview)
}

func CheckReadOnly(dialect sqlparse.Dialect, sql string) error {
	parsed, err := sqlparse.ParseSQL(dialect, sql)
	if err != nil {
		return err
	}
	if err := sqlpolicy.CheckReadOnlySingleStatement(parsed.Statements); err != nil {
		switch typed := err.(type) {
		case *sqlpolicy.ErrReadOnlyStatementCount:
			return typed
		case *sqlpolicy.ErrReadOnlyViolation:
			return &ErrReadOnlyViolation{
				Statement: typed.Statement,
				Keyword:   string(typed.Kind),
			}
		default:
			return err
		}
	}
	return nil
}
