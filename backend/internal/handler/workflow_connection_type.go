package handler

import (
	"fmt"
	"strings"

	"github.com/dbre-maestro/maestro/internal/model"
)

func validateTicketDBType(ticketType model.TicketType, dbType string) error {
	normalized := strings.ToLower(strings.TrimSpace(dbType))
	switch ticketType {
	case model.TicketTypeRedisCommand:
		if normalized != "redis" {
			return fmt.Errorf("redis_command tickets only support redis connections")
		}
	case model.TicketTypeDDL, model.TicketTypeDML, model.TicketTypeSQLExport, model.TicketTypeSensitiveQueryAccess:
		if normalized != "mysql" && normalized != "postgres" && normalized != "postgresql" {
			return fmt.Errorf("%s tickets only support mysql and postgres connections", ticketType)
		}
	case model.TicketTypeQueryAccess:
		if normalized != "mysql" && normalized != "postgres" && normalized != "postgresql" && normalized != "redis" {
			return fmt.Errorf("query_access tickets only support mysql, postgres, and redis connections")
		}
	}
	return nil
}
