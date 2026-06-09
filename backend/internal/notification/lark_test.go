package notification_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbre-maestro/maestro/internal/notification"
)

func newClient(t *testing.T, url string) *notification.Client {
	t.Helper()
	return notification.NewClient(notification.Config{
		Mode:       notification.ModeWebhook,
		WebhookURL: url,
	})
}

func TestSendSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	res := c.Send(context.Background(), notification.Message{Title: "test", Body: "ok"})
	if res.Err != nil {
		t.Fatalf("expected success, got: %v", res.Err)
	}
	if res.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", res.Attempts)
	}
}

func TestSend5xxRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	// Shorten retry delays for test speed via context timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := c.Send(ctx, notification.Message{Title: "test", Body: "retry"})
	if res.Err == nil {
		t.Fatal("expected error after 3 retries")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestSend4xxNoRetry(t *testing.T) {
	// TE7: 4xx must not retry
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	res := c.Send(context.Background(), notification.Message{Title: "test", Body: "forbidden"})
	if res.Err == nil {
		t.Fatal("expected error on 403")
	}
	if calls.Load() != 1 {
		t.Errorf("4xx should not retry: got %d calls, want 1", calls.Load())
	}
}

func TestSend4xxCodes(t *testing.T) {
	// TE7: 401/403/404 all fail immediately — 1 attempt, no retry.
	// When Err != nil, the caller is responsible for logging notification_failure to audit_logs.
	for _, code := range []int{
		http.StatusUnauthorized, // 401 — token revoked / wrong secret
		http.StatusForbidden,    // 403 — IP/scope rejected
		http.StatusNotFound,     // 404 — webhook URL no longer exists
	} {
		code := code
		t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			c := newClient(t, srv.URL)
			res := c.Send(context.Background(), notification.Message{Title: "t", Body: "b"})

			if res.Err == nil {
				t.Fatalf("status %d: expected Err != nil (signals audit logging needed)", code)
			}
			if calls.Load() != 1 {
				t.Errorf("status %d: must not retry — got %d calls, want 1", code, calls.Load())
			}
			if res.Attempts != 1 {
				t.Errorf("status %d: expected Attempts=1, got %d", code, res.Attempts)
			}
			// Error must include the status code so audit log callers have context
			if !strings.Contains(res.Err.Error(), fmt.Sprintf("%d", code)) {
				t.Errorf("status %d: error should reference status code, got: %s", code, res.Err.Error())
			}
		})
	}
}

func TestSend5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res := c.Send(ctx, notification.Message{Title: "test", Body: "eventually ok"})
	if res.Err != nil {
		t.Fatalf("expected success on 3rd attempt, got: %v", res.Err)
	}
	if res.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", res.Attempts)
	}
}
