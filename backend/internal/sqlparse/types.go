package sqlparse

type Dialect string

const (
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
	DialectGeneric  Dialect = "generic"
)

type StatementKind string

const (
	StatementKindUnknown  StatementKind = "unknown"
	StatementKindSelect   StatementKind = "select"
	StatementKindShow     StatementKind = "show"
	StatementKindExplain  StatementKind = "explain"
	StatementKindDescribe StatementKind = "describe"
	StatementKindInsert   StatementKind = "insert"
	StatementKindUpdate   StatementKind = "update"
	StatementKindDelete   StatementKind = "delete"
	StatementKindSet      StatementKind = "set"
	StatementKindCreate   StatementKind = "create"
	StatementKindAlter    StatementKind = "alter"
	StatementKindDrop     StatementKind = "drop"
	StatementKindTruncate StatementKind = "truncate"
)

type ParsedStatement struct {
	Seq           int
	RawSQL        string
	NormalizedSQL string
	Kind          StatementKind
	AST           any
}

type ParseResult struct {
	Statements []ParsedStatement
}
