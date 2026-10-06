package onlineddl

import (
	"errors"
	"strings"
	"testing"
)

func TestParseApprovedAlterUsesASTIdentityAndClause(t *testing.T) {
	stmt, err := ParseApprovedAlter("ALTER TABLE `audit-db`.`order``items` ADD COLUMN `note` varchar(20), DROP COLUMN `old`", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	if stmt.Database != "audit-db" || stmt.Table != "order`items" {
		t.Fatalf("identity = %q.%q", stmt.Database, stmt.Table)
	}
	if !strings.Contains(stmt.AlterClause, "ADD COLUMN `note`") || strings.Contains(stmt.AlterClause, "audit-db") {
		t.Fatalf("clause = %q", stmt.AlterClause)
	}
	fallback, err := ParseApprovedAlter("ALTER TABLE `orders` ADD INDEX (`id`)", "billing")
	if err != nil || fallback.Database != "billing" {
		t.Fatalf("fallback = %+v, %v", fallback, err)
	}
}

func TestParseApprovedAlterRejectsSubstitutionSurfaces(t *testing.T) {
	for _, sql := range []string{"UPDATE orders SET x=1", "ALTER TABLE a ADD x int; ALTER TABLE b ADD y int", "ALTER TABLE orders"} {
		if _, err := ParseApprovedAlter(sql, "billing"); !errors.Is(err, ErrUnsupportedStatement) {
			t.Fatalf("sql %q error = %v", sql, err)
		}
	}
}

func TestTypedParameterBoundariesAndMutualExclusion(t *testing.T) {
	ghost := DefaultParameters(ModeGhost)
	if err := ghost.Validate(ModeGhost); err != nil {
		t.Fatal(err)
	}
	ghost.Ghost.CriticalLoadThreadsRunning = ghost.Ghost.MaxLoadThreadsRunning
	if !errors.Is(ghost.Validate(ModeGhost), ErrInvalidParameters) {
		t.Fatal("critical load must exceed max load")
	}
	ptosc := DefaultParameters(ModePTOSC)
	chunkTime := .5
	ptosc.PTOSC.ChunkTime = &chunkTime
	if !errors.Is(ptosc.Validate(ModePTOSC), ErrInvalidParameters) {
		t.Fatal("chunk size and chunk time must be mutually exclusive")
	}
	ptosc.PTOSC.ChunkSize = nil
	if err := ptosc.Validate(ModePTOSC); err != nil {
		t.Fatal(err)
	}
}
