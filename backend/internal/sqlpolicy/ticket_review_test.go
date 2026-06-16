package sqlpolicy

import (
	"testing"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/sqlparse"
)

func TestCheckTicketStatementKinds(t *testing.T) {
	t.Run("ddl accepts ddl only", func(t *testing.T) {
		err := CheckTicketStatementKinds(model.TicketTypeDDL, []sqlparse.ParsedStatement{
			{Seq: 1, Kind: sqlparse.StatementKindCreate},
			{Seq: 2, Kind: sqlparse.StatementKindAlter},
		})
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("ddl rejects dml", func(t *testing.T) {
		err := CheckTicketStatementKinds(model.TicketTypeDDL, []sqlparse.ParsedStatement{
			{Seq: 1, Kind: sqlparse.StatementKindCreate},
			{Seq: 2, Kind: sqlparse.StatementKindUpdate},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("dml accepts dml only", func(t *testing.T) {
		err := CheckTicketStatementKinds(model.TicketTypeDML, []sqlparse.ParsedStatement{
			{Seq: 1, Kind: sqlparse.StatementKindUpdate},
			{Seq: 2, Kind: sqlparse.StatementKindDelete},
		})
		if err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("dml rejects ddl", func(t *testing.T) {
		err := CheckTicketStatementKinds(model.TicketTypeDML, []sqlparse.ParsedStatement{
			{Seq: 1, Kind: sqlparse.StatementKindUpdate},
			{Seq: 2, Kind: sqlparse.StatementKindCreate},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
