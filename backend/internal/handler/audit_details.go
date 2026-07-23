package handler

import (
	"context"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

func auditConnectionDetails(conn *model.DBConnection) map[string]any {
	if conn == nil {
		return map[string]any{}
	}
	details := map[string]any{
		"connection_id":   conn.ID,
		"connection_name": conn.Name,
		"db_type":         conn.DBType,
	}
	if conn.DatabaseName != nil && strings.TrimSpace(*conn.DatabaseName) != "" {
		details["default_database_name"] = strings.TrimSpace(*conn.DatabaseName)
	}
	return details
}

func addAuditConnectionDetails(details map[string]any, conn *model.DBConnection) {
	for key, value := range auditConnectionDetails(conn) {
		details[key] = value
	}
}

func addAuditTicketDetails(details map[string]any, ticket *model.Ticket) {
	if ticket == nil {
		return
	}
	details["ticket_id"] = ticket.ID
	details["ticket_no"] = ticket.TicketNo
	details["ticket_type"] = ticket.TicketType
	details["ticket_title"] = ticket.Title
	if ticket.DatabaseName != nil && strings.TrimSpace(*ticket.DatabaseName) != "" {
		details["database_name"] = strings.TrimSpace(*ticket.DatabaseName)
	}
	if ticket.SchemaName != nil && strings.TrimSpace(*ticket.SchemaName) != "" {
		details["schema_name"] = strings.TrimSpace(*ticket.SchemaName)
	}
}

func addAuditQueryContextDetails(details map[string]any, queryCtx queryExecutionContext) {
	if strings.TrimSpace(queryCtx.DatabaseName) != "" {
		details["database_name"] = strings.TrimSpace(queryCtx.DatabaseName)
	}
	if strings.TrimSpace(queryCtx.SchemaName) != "" {
		details["schema_name"] = strings.TrimSpace(queryCtx.SchemaName)
	}
	if queryCtx.RedisDBIndex != nil {
		details["redis_db_index"] = *queryCtx.RedisDBIndex
	}
}

func addAuditUserListDetails(ctx context.Context, details map[string]any, users *repository.UserRepo, key string, ids []uint64) {
	details[key+"_ids"] = ids
	if users == nil || len(ids) == 0 {
		return
	}
	records, err := users.ListByIDs(ctx, ids)
	if err != nil {
		return
	}
	names := make([]string, 0, len(records))
	for _, user := range records {
		if strings.TrimSpace(user.Username) != "" {
			names = append(names, user.Username)
		}
	}
	if len(names) > 0 {
		details[key+"_names"] = names
	}
}
