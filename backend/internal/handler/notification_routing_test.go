package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/notification"
)

func TestNotificationRouterSendDoesNotWaitForLark(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	requestCompleted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		w.WriteHeader(http.StatusOK)
		close(requestCompleted)
	}))
	defer server.Close()

	router := NewNotificationRouter(nil, nil, nil, nil, notification.NewDispatcher(nil, nil, server.URL))
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() {
		router.SendTicket(ctx, &model.Ticket{ID: 42, TicketNo: "TK-42"}, NotificationRoute{
			RecipientIDs: []uint64{7},
			NotifType:    "ticket_pending_review",
			Title:        "Ticket pending review",
			Body:         "Review requested",
		})
		close(returned)
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("Lark request did not start")
	}
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Send waited for the Lark request")
	}

	cancel()
	close(releaseRequest)
	select {
	case <-requestCompleted:
	case <-time.After(time.Second):
		t.Fatal("detached Lark request did not complete after caller context cancellation")
	}
}
