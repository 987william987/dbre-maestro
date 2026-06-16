package sqlparse

import (
	"strings"
	"unicode"
)

func DialectFromDBType(dbType string) Dialect {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mysql":
		return DialectMySQL
	case "postgres", "postgresql":
		return DialectPostgres
	default:
		return DialectGeneric
	}
}

func ParseSQL(dialect Dialect, sql string) (*ParseResult, error) {
	switch dialect {
	case DialectMySQL:
		return parseMySQL(sql)
	case DialectPostgres:
		return parsePostgres(sql)
	default:
		return parseGeneric(sql)
	}
}

func parseGeneric(sql string) (*ParseResult, error) {
	statements := splitStatements(stripComments(sql))
	result := &ParseResult{Statements: make([]ParsedStatement, 0, len(statements))}
	for index, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		result.Statements = append(result.Statements, ParsedStatement{
			Seq:           len(result.Statements) + 1,
			RawSQL:        trimmed,
			NormalizedSQL: trimmed,
			Kind:          classifyGenericStatement(trimmed),
		})
		_ = index
	}
	return result, nil
}

func classifyGenericStatement(stmt string) StatementKind {
	kw := firstKeyword(stmt)
	switch kw {
	case "SELECT":
		return StatementKindSelect
	case "WITH":
		return classifyGenericStatement(mainKeywordAfterWith(stmt))
	case "SHOW":
		return StatementKindShow
	case "EXPLAIN":
		return StatementKindExplain
	case "DESC", "DESCRIBE":
		return StatementKindDescribe
	case "INSERT", "REPLACE":
		return StatementKindInsert
	case "UPDATE":
		return StatementKindUpdate
	case "DELETE":
		return StatementKindDelete
	case "CREATE":
		return StatementKindCreate
	case "ALTER", "RENAME":
		return StatementKindAlter
	case "DROP":
		return StatementKindDrop
	case "TRUNCATE":
		return StatementKindTruncate
	default:
		return StatementKindUnknown
	}
}

func mainKeywordAfterWith(stmt string) string {
	upper := strings.ToUpper(stmt)
	pos := skipToken(upper, 0)
	pos = skipWhitespace(upper, pos)
	if strings.HasPrefix(upper[pos:], "RECURSIVE") {
		pos = skipToken(upper, pos)
		pos = skipWhitespace(upper, pos)
	}
	for pos < len(upper) {
		pos = skipToken(upper, pos)
		pos = skipWhitespace(upper, pos)
		if pos >= len(upper) {
			break
		}
		if strings.HasPrefix(upper[pos:], "AS") {
			pos = skipToken(upper, pos)
			pos = skipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == '(' {
			pos = skipBalancedParen(upper, pos)
			pos = skipWhitespace(upper, pos)
		}
		if pos < len(upper) && upper[pos] == ',' {
			pos++
			pos = skipWhitespace(upper, pos)
			continue
		}
		break
	}
	return firstKeyword(strings.TrimSpace(stmt[pos:]))
}

func stripComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		switch {
		case i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			}
		case sql[i] == '\'' || sql[i] == '"' || sql[i] == '`':
			q := sql[i]
			b.WriteByte(q)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == q {
					if i+1 < len(sql) && sql[i+1] == q {
						i++
						b.WriteByte(sql[i])
					} else {
						i++
						break
					}
				}
				i++
			}
		default:
			b.WriteByte(sql[i])
			i++
		}
	}
	return b.String()
}

func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	depth := 0
	var quote rune

	for _, ch := range sql {
		switch {
		case quote != 0:
			current.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
			current.WriteRune(ch)
		case ch == '(':
			depth++
			current.WriteRune(ch)
		case ch == ')':
			if depth > 0 {
				depth--
			}
			current.WriteRune(ch)
		case ch == ';' && depth == 0:
			if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
				statements = append(statements, trimmed)
			}
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		statements = append(statements, trimmed)
	}
	return statements
}

func firstKeyword(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	i := 0
	for i < len(stmt) && (unicode.IsLetter(rune(stmt[i])) || stmt[i] == '_') {
		i++
	}
	if i == 0 {
		return ""
	}
	return strings.ToUpper(stmt[:i])
}

func skipWhitespace(s string, pos int) int {
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
		pos++
	}
	return pos
}

func skipToken(s string, pos int) int {
	pos = skipWhitespace(s, pos)
	for pos < len(s) && (unicode.IsLetter(rune(s[pos])) || s[pos] == '_' || unicode.IsDigit(rune(s[pos]))) {
		pos++
	}
	return pos
}

func skipBalancedParen(s string, pos int) int {
	if pos >= len(s) || s[pos] != '(' {
		return pos
	}
	depth := 1
	pos++
	for pos < len(s) && depth > 0 {
		switch s[pos] {
		case '(':
			depth++
		case ')':
			depth--
		}
		pos++
	}
	return pos
}
