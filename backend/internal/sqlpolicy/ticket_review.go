package sqlpolicy

import (
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

type ErrTicketStatementKind struct {
	StatementSeq int
	Kind         sqlparse.StatementKind
	TicketType   model.TicketType
}

func (e *ErrTicketStatementKind) Error() string {
	return fmt.Sprintf("statement %d is %s, but ticket_type=%s only allows %s statements", e.StatementSeq, e.Kind, e.TicketType, expectedTicketKind(e.TicketType))
}

func CheckTicketStatementKinds(ticketType model.TicketType, statements []sqlparse.ParsedStatement) error {
	for _, stmt := range statements {
		switch ticketType {
		case model.TicketTypeDDL:
			if !isDDLKind(stmt.Kind) {
				return &ErrTicketStatementKind{StatementSeq: stmt.Seq, Kind: stmt.Kind, TicketType: ticketType}
			}
		case model.TicketTypeDML:
			if !isDMLKind(stmt.Kind) {
				return &ErrTicketStatementKind{StatementSeq: stmt.Seq, Kind: stmt.Kind, TicketType: ticketType}
			}
		}
	}
	return nil
}

func isDDLKind(kind sqlparse.StatementKind) bool {
	switch kind {
	case sqlparse.StatementKindCreate, sqlparse.StatementKindAlter, sqlparse.StatementKindDrop, sqlparse.StatementKindTruncate:
		return true
	default:
		return false
	}
}

func isDMLKind(kind sqlparse.StatementKind) bool {
	switch kind {
	case sqlparse.StatementKindInsert, sqlparse.StatementKindUpdate, sqlparse.StatementKindDelete:
		return true
	default:
		return false
	}
}

func expectedTicketKind(ticketType model.TicketType) string {
	switch ticketType {
	case model.TicketTypeDDL:
		return "DDL"
	case model.TicketTypeDML:
		return "DML"
	default:
		return "matching"
	}
}
