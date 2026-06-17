package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbre-maestro/maestro/internal/middleware"
	"github.com/dbre-maestro/maestro/internal/realtime"
)

type EventStreamHandler struct {
	broker *realtime.Broker
}

func NewEventStreamHandler(broker *realtime.Broker) *EventStreamHandler {
	return &EventStreamHandler{broker: broker}
}

func (h *EventStreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	userID := middleware.UserIDFromCtx(r.Context())
	if userID == 0 || h.broker == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, cancel := h.broker.Subscribe(userID, 16)
	defer cancel()

	fmt.Fprint(w, "event: ready\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			if len(event.Data) == 0 {
				fmt.Fprintf(w, "event: %s\ndata: {}\n\n", event.Type)
			} else {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
			}
			flusher.Flush()
		}
	}
}
