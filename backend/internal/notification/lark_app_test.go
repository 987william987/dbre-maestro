package notification

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendToRecipientUsesOpenID(t *testing.T) {
	t.Helper()

	var seenMessageRequest bool

	client := NewClient(Config{
		Mode:      ModeApp,
		AppID:     "cli_test",
		AppSecret: "secret_test",
	})
	client.http = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.String() == "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal":
				return jsonResponse(http.StatusOK, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
			case strings.HasPrefix(req.URL.String(), "https://open.larksuite.com/open-apis/im/v1/messages"):
				seenMessageRequest = true
				if got := req.URL.Query().Get("receive_id_type"); got != "open_id" {
					t.Fatalf("receive_id_type = %q, want open_id", got)
				}
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				payload := string(body)
				if !strings.Contains(payload, `"receive_id":"ou_test_recipient"`) {
					t.Fatalf("receive_id payload mismatch: %s", payload)
				}
				return jsonResponse(http.StatusOK, `{"code":0,"msg":"success"}`), nil
			default:
				t.Fatalf("unexpected request: %s", req.URL.String())
				return nil, nil
			}
		}),
	}

	result := client.SendToRecipient(context.Background(), "ou_test_recipient", Message{Title: "ticket", Body: "hello"})
	if result.Err != nil {
		t.Fatalf("expected success, got error: %v", result.Err)
	}
	if result.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", result.Attempts)
	}
	if !seenMessageRequest {
		t.Fatal("expected message request to be sent")
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{URL: &url.URL{}},
	}
}
