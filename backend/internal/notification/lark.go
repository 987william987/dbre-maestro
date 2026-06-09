package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Mode string

const (
	ModeWebhook Mode = "webhook"
	ModeApp     Mode = "app"
)

// Config holds Lark notification settings (loaded from platform_settings or env).
type Config struct {
	Mode       Mode
	WebhookURL string
	AppID      string
	AppSecret  string
}

type Message struct {
	Title   string
	Body    string
	TicketNo string // optional, included in body when set
}

// Client sends Lark notifications with retry logic.
// T3: 5xx → exponential backoff 1s→3s→9s, max 3 retries.
// TE7: 4xx (401/403/404) → direct failure, no retry.
type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// retryDelays defines the wait before attempt 2, 3, 4 (index = attempt-1).
var retryDelays = []time.Duration{
	1 * time.Second,
	3 * time.Second,
	9 * time.Second,
}

const maxAttempts = 3

// SendResult is returned by Send regardless of success/failure so callers
// can log to audit_logs without needing to inspect the error type.
type SendResult struct {
	Attempts int
	Err      error
}

// Send delivers a message. The caller is responsible for logging failures
// to audit_logs (pass SendResult.Err != nil → action_type='notification_failure').
func (c *Client) Send(ctx context.Context, msg Message) SendResult {
	payload := c.buildPayload(msg)
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(retryDelays[attempt-2]):
			case <-ctx.Done():
				return SendResult{Attempts: attempt - 1, Err: ctx.Err()}
			}
		}

		status, err := c.post(ctx, payload)
		if err != nil {
			// Network / transport error → retry
			lastErr = fmt.Errorf("attempt %d network error: %w", attempt, err)
			continue
		}

		if status >= 200 && status < 300 {
			return SendResult{Attempts: attempt}
		}

		// TE7: 4xx → direct failure, no retry
		if status >= 400 && status < 500 {
			return SendResult{
				Attempts: attempt,
				Err:      fmt.Errorf("lark 4xx (status %d): not retrying", status),
			}
		}

		// 5xx → retry
		lastErr = fmt.Errorf("attempt %d lark 5xx (status %d)", attempt, status)
	}

	return SendResult{
		Attempts: maxAttempts,
		Err:      fmt.Errorf("lark notification failed after %d attempts: %w", maxAttempts, lastErr),
	}
}

func (c *Client) post(ctx context.Context, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// larkTextMsg is the minimal Lark webhook payload (text mode).
type larkTextMsg struct {
	MsgType string      `json:"msg_type"`
	Content larkContent `json:"content"`
}

type larkContent struct {
	Text string `json:"text"`
}

func (c *Client) buildPayload(msg Message) []byte {
	text := fmt.Sprintf("【%s】%s", msg.Title, msg.Body)
	if msg.TicketNo != "" {
		text = fmt.Sprintf("【%s】工單 %s — %s", msg.Title, msg.TicketNo, msg.Body)
	}
	b, _ := json.Marshal(larkTextMsg{
		MsgType: "text",
		Content: larkContent{Text: text},
	})
	return b
}
