package handler

import (
	"context"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
	"github.com/dbre-maestro/maestro/internal/realtime"
	"github.com/dbre-maestro/maestro/internal/repository"
)

type NotificationRouter struct {
	notifs *repository.NotificationRepo
	audit  *repository.AuditRepo
	broker *realtime.Broker
	lark   *notification.Dispatcher
}

type NotificationRoute struct {
	RecipientIDs []uint64
	ActorID      *uint64
	NotifyActor  bool
	NotifType    string
	Title        string
	Body         string
	ResourceType string
	ResourceID   uint64
	TicketNo     string
}

func NewNotificationRouter(notifs *repository.NotificationRepo, audit *repository.AuditRepo, broker *realtime.Broker, lark *notification.Dispatcher) *NotificationRouter {
	return &NotificationRouter{notifs: notifs, audit: audit, broker: broker, lark: lark}
}

func (r *NotificationRouter) Send(ctx context.Context, route NotificationRoute) []uint64 {
	recipients := routeRecipients(route.RecipientIDs, route.ActorID, route.NotifyActor)
	inAppCreated := []uint64{}
	inAppFailed := []uint64{}
	if r.notifs != nil {
		for _, userID := range recipients {
			resourceType := route.ResourceType
			notificationID, err := r.notifs.Create(ctx, userID, route.NotifType, route.Title, route.Body, &resourceType, &route.ResourceID)
			if err != nil {
				inAppFailed = append(inAppFailed, userID)
				r.recordDelivery(ctx, route, userID, "in_app", "failed", 0, err.Error())
				continue
			}
			inAppCreated = append(inAppCreated, userID)
			r.recordDelivery(ctx, route, userID, "in_app", "sent", 1, "")
			publishNotificationCreated(ctx, r.broker, r.notifs, userID, notificationID)
		}
	}

	larkStatus := "skipped"
	larkAttempts := 0
	larkError := ""
	larkSkippedReason := ""
	if len(recipients) == 0 {
		larkSkippedReason = "no_recipients"
	}
	if r.lark != nil && len(recipients) > 0 {
		result := r.lark.NotifyUsers(ctx, recipients, notification.Message{Title: route.Title, Body: route.Body, TicketNo: route.TicketNo})
		larkAttempts = result.Attempts
		if result.Err != nil {
			larkStatus = "failed"
			larkError = result.Err.Error()
			r.log(ctx, repository.AuditEntry{
				ActionType:   "notification_failure",
				ResourceType: route.ResourceType,
				ResourceID:   &route.ResourceID,
				Details: map[string]any{
					"type":       route.NotifType,
					"title":      route.Title,
					"recipients": recipients,
					"err":        larkError,
					"attempts":   larkAttempts,
				},
			})
		} else if result.Attempts == 0 {
			larkStatus = "skipped"
			larkSkippedReason = result.SkippedReason
			if larkSkippedReason == "" {
				larkSkippedReason = "lark_skipped"
			}
		} else {
			larkStatus = "sent"
		}
	} else if r.lark == nil && len(recipients) > 0 {
		larkSkippedReason = "lark_dispatcher_not_configured"
	}
	for _, userID := range recipients {
		errorMessage := larkError
		if larkStatus == "skipped" {
			errorMessage = larkSkippedReason
		}
		r.recordDelivery(ctx, route, userID, "lark", larkStatus, larkAttempts, errorMessage)
	}

	r.log(ctx, repository.AuditEntry{
		ActionType:   "notification_delivery",
		ResourceType: route.ResourceType,
		ResourceID:   &route.ResourceID,
		Details: map[string]any{
			"type":                 route.NotifType,
			"title":                route.Title,
			"intended_recipients":  route.RecipientIDs,
			"delivered_recipients": recipients,
			"in_app_created_users": inAppCreated,
			"in_app_failed_users":  inAppFailed,
			"lark_status":          larkStatus,
			"lark_skipped_reason":  larkSkippedReason,
			"lark_attempts":        larkAttempts,
			"lark_error":           larkError,
			"actor_excluded":       route.ActorID != nil && !route.NotifyActor,
			"notification_channel": "in_app,lark",
		},
	})
	return recipients
}

func (r *NotificationRouter) SendTicket(ctx context.Context, ticket *model.Ticket, route NotificationRoute) []uint64 {
	if ticket != nil {
		route.ResourceType = "ticket"
		route.ResourceID = ticket.ID
		route.TicketNo = ticket.TicketNo
	}
	return r.Send(ctx, route)
}

func (r *NotificationRouter) log(ctx context.Context, entry repository.AuditEntry) {
	if r == nil || r.audit == nil {
		return
	}
	r.audit.Log(ctx, entry)
}

func (r *NotificationRouter) recordDelivery(ctx context.Context, route NotificationRoute, userID uint64, channel string, status string, attempts int, errMsg string) {
	if r == nil || r.notifs == nil {
		return
	}
	_ = r.notifs.CreateDelivery(ctx, repository.NotificationDelivery{
		NotificationType: route.NotifType,
		ResourceType:     route.ResourceType,
		ResourceID:       route.ResourceID,
		UserID:           userID,
		Channel:          channel,
		Status:           status,
		Attempts:         attempts,
		ErrorMessage:     errMsg,
	})
}

func routeRecipients(userIDs []uint64, actorID *uint64, notifyActor bool) []uint64 {
	seen := map[uint64]struct{}{}
	recipients := make([]uint64, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == 0 {
			continue
		}
		if actorID != nil && !notifyActor && *actorID == userID {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		recipients = append(recipients, userID)
	}
	return recipients
}
