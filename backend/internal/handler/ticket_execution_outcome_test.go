package handler

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyTicketStatementExecutionError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		sentToDB   bool
		wantReason string
		wantResult string
	}{
		{
			name:       "connection failure before statement is sent",
			err:        errors.New("connection refused"),
			sentToDB:   false,
			wantReason: ticketExecutionOutcomeNotSent,
			wantResult: ticketExecutionOutcomeNotSent,
		},
		{
			name:       "mysql explicit db error after statement is sent",
			err:        &mysql.MySQLError{Number: 1142, Message: "SELECT command denied"},
			sentToDB:   true,
			wantReason: "",
			wantResult: ticketExecutionOutcomeFailed,
		},
		{
			name:       "postgres explicit db error after statement is sent",
			err:        &pgconn.PgError{Code: "42P01", Message: "relation does not exist"},
			sentToDB:   true,
			wantReason: "",
			wantResult: ticketExecutionOutcomeFailed,
		},
		{
			name:       "connection interruption after statement is sent",
			err:        driver.ErrBadConn,
			sentToDB:   true,
			wantReason: "connection_interrupted",
			wantResult: ticketExecutionOutcomeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReason, gotResult := classifyTicketStatementExecutionError(tt.err, tt.sentToDB)
			if gotReason != tt.wantReason || gotResult != tt.wantResult {
				t.Fatalf("classifyTicketStatementExecutionError() = (%q, %q), want (%q, %q)", gotReason, gotResult, tt.wantReason, tt.wantResult)
			}
		})
	}
}
