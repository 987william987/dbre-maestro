package handler

import (
	"context"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/realtime"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type notificationCreatedEvent struct {
	Notification model.Notification `json:"notification"`
}

type ticketUpdatedEvent struct {
	TicketID uint64 `json:"ticket_id"`
	Status   string `json:"status,omitempty"`
}

func publishNotificationCreated(ctx context.Context, broker *realtime.Broker, repo *repository.NotificationRepo, userID uint64, notificationID uint64) {
	if broker == nil || repo == nil || userID == 0 || notificationID == 0 {
		return
	}
	notification, err := repo.GetByIDForUser(ctx, notificationID, userID)
	if err != nil || notification == nil {
		return
	}
	broker.PublishToUsers([]uint64{userID}, "notification.created", notificationCreatedEvent{Notification: *notification})
}

func publishTicketRealtimeEvent(ctx context.Context, broker *realtime.Broker, users *repository.UserRepo, ticket *model.Ticket, actorID *uint64) {
	if broker == nil || users == nil || ticket == nil {
		return
	}

	recipients := make([]uint64, 0, 8)
	recipients = append(recipients, ticket.SubmitterID)
	if actorID != nil && *actorID != 0 {
		recipients = append(recipients, *actorID)
	}
	if reviewerIDs, err := listActiveUserIDsByPermissions(ctx, users, reviewPermissionsForTicket(ticket.TicketType)); err == nil {
		recipients = append(recipients, reviewerIDs...)
	}
	if executorIDs, err := listActiveUserIDsByPermissions(ctx, users, []string{permissionTicketExecute}); err == nil {
		recipients = append(recipients, executorIDs...)
	}
	if ticket.ExecutorID != nil {
		recipients = append(recipients, *ticket.ExecutorID)
	}

	broker.PublishToUsers(recipients, "ticket.updated", ticketUpdatedEvent{
		TicketID: ticket.ID,
		Status:   string(ticket.Status),
	})
}
