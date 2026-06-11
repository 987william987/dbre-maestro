package handler

import (
	"testing"
	"github.com/dbre-maestro/maestro/internal/model"
)

func TestConnectionDatabaseNameTrimsWhitespace(t *testing.T) {
	name := "  maestro  "
	conn := &model.DBConnection{DatabaseName: &name}

	if got := connectionDatabaseName(conn); got != "maestro" {
		t.Fatalf("connectionDatabaseName() = %q, want %q", got, "maestro")
	}
}

func TestBuildRedisMetadataItems(t *testing.T) {
	items := buildRedisMetadataItems()

	if len(items) != 16 {
		t.Fatalf("len(items) = %d, want 16", len(items))
	}
	if items[0].Kind != "redis_db" || items[0].Name != "0" {
		t.Fatalf("items[0] = %#v", items[0])
	}
	if items[15].Name != "15" {
		t.Fatalf("items[15].Name = %q, want %q", items[15].Name, "15")
	}
}
