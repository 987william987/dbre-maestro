package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	Title    string
	Body     string
	TicketNo string // optional, included in body when set
}

// Client sends Lark notifications with retry logic.
// T3: 5xx → exponential backoff 1s→3s→9s, max 3 retries.
// TE7: 4xx (401/403/404) → direct failure, no retry.
type Client struct {
	cfg         Config
	http        *http.Client
	tokenMu     sync.Mutex
	accessToken string
	tokenExpiry time.Time
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
	if c.cfg.Mode == ModeApp {
		return SendResult{Err: fmt.Errorf("app mode requires a recipient")}
	}
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

func (c *Client) SendToEmail(ctx context.Context, email string, msg Message) SendResult {
	email = strings.TrimSpace(email)
	if email == "" {
		return SendResult{}
	}
	if c.cfg.Mode == ModeWebhook {
		return c.Send(ctx, msg)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(retryDelays[attempt-2]):
			case <-ctx.Done():
				return SendResult{Attempts: attempt - 1, Err: ctx.Err()}
			}
		}

		status, err := c.postAppMessage(ctx, "email", email, msg)
		if err != nil {
			var apiErr *larkAPIError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				return SendResult{
					Attempts: attempt,
					Err:      err,
				}
			}
			lastErr = fmt.Errorf("attempt %d app error: %w", attempt, err)
			continue
		}
		if status >= 200 && status < 300 {
			return SendResult{Attempts: attempt}
		}
		if status >= 400 && status < 500 {
			return SendResult{
				Attempts: attempt,
				Err:      fmt.Errorf("lark 4xx (status %d): not retrying", status),
			}
		}
		lastErr = fmt.Errorf("attempt %d lark 5xx (status %d)", attempt, status)
	}

	return SendResult{
		Attempts: maxAttempts,
		Err:      fmt.Errorf("lark app notification failed after %d attempts: %w", maxAttempts, lastErr),
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

type larkTenantTokenRequest struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type larkTenantTokenResponse struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

type larkSendMessageRequest struct {
	ReceiveID string `json:"receive_id"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
}

type larkSendMessageResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type larkAPIError struct {
	Status int
	Body   string
}

func (e *larkAPIError) Error() string {
	return e.Body
}

func (c *Client) buildPayload(msg Message) []byte {
	text := c.buildText(msg)
	b, _ := json.Marshal(larkTextMsg{
		MsgType: "text",
		Content: larkContent{Text: text},
	})
	return b
}

func (c *Client) buildText(msg Message) string {
	text := fmt.Sprintf("【%s】%s", msg.Title, msg.Body)
	if msg.TicketNo != "" {
		text = fmt.Sprintf("【%s】工單 %s — %s", msg.Title, msg.TicketNo, msg.Body)
	}
	return text
}

func (c *Client) postAppMessage(ctx context.Context, receiveIDType, receiveID string, msg Message) (int, error) {
	accessToken, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return 0, err
	}

	content, err := json.Marshal(larkContent{Text: c.buildText(msg)})
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(larkSendMessageRequest{
		ReceiveID: receiveID,
		MsgType:   "text",
		Content:   string(content),
	})
	if err != nil {
		return 0, err
	}

	endpoint := "https://open.larksuite.com/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var parsed larkSendMessageResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &parsed); err != nil {
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return resp.StatusCode, &larkAPIError{
					Status: resp.StatusCode,
					Body:   fmt.Sprintf("lark send message http %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
				}
			}
			return 0, fmt.Errorf("decode lark message response: %w", err)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(parsed.Msg)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, &larkAPIError{
			Status: resp.StatusCode,
			Body:   fmt.Sprintf("lark send message http %d code=%d msg=%s", resp.StatusCode, parsed.Code, msg),
		}
	}
	if parsed.Code != 0 {
		return http.StatusBadRequest, &larkAPIError{
			Status: http.StatusBadRequest,
			Body:   fmt.Sprintf("lark app error code %d: %s", parsed.Code, parsed.Msg),
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) getTenantAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.accessToken
		c.tokenMu.Unlock()
		return token, nil
	}
	c.tokenMu.Unlock()

	payload, err := json.Marshal(larkTenantTokenRequest{
		AppID:     c.cfg.AppID,
		AppSecret: c.cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.larksuite.com/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tenant access token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed larkTenantTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode tenant access token response: %w", err)
	}
	if parsed.Code != 0 {
		return "", fmt.Errorf("tenant access token error code %d: %s", parsed.Code, parsed.Msg)
	}
	if parsed.TenantAccessToken == "" {
		return "", fmt.Errorf("tenant access token is empty")
	}

	expiry := time.Now().Add(time.Duration(parsed.Expire-60) * time.Second)
	if parsed.Expire <= 60 {
		expiry = time.Now().Add(30 * time.Second)
	}

	c.tokenMu.Lock()
	c.accessToken = parsed.TenantAccessToken
	c.tokenExpiry = expiry
	c.tokenMu.Unlock()
	return parsed.TenantAccessToken, nil
}
