package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	Card     *Card
}

type Card struct {
	Title          string
	Template       string
	Fields         []CardField
	MarkdownBlocks []string
	Actions        []CardAction
}

type CardField struct {
	Label string
	Value string
}

type CardAction struct {
	Action string
	Text   string
	Type   string
	URL    string
	Value  map[string]any
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
	Attempts      int
	Err           error
	SkippedReason string
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

// SendToRecipient sends an app message to a Lark open_id recipient.
func (c *Client) SendToRecipient(ctx context.Context, recipient string, msg Message) SendResult {
	return c.SendToRecipientType(ctx, "open_id", recipient, msg)
}

// SendToRecipientType sends an app message to a Lark recipient using receive_id_type.
func (c *Client) SendToRecipientType(ctx context.Context, recipientType string, recipient string, msg Message) SendResult {
	recipientType = normalizeReceiveIDType(recipientType)
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
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

		status, err := c.postAppMessage(ctx, recipientType, recipient, msg)
		if err != nil {
			var apiErr *larkAPIError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				if apiErr.isInvalidAccessToken() {
					c.invalidateTenantAccessToken()
					lastErr = fmt.Errorf("attempt %d invalid access token: %w", attempt, err)
					continue
				}
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

func normalizeReceiveIDType(value string) string {
	switch strings.TrimSpace(value) {
	case "union_id":
		return "union_id"
	default:
		return "open_id"
	}
}

func (c *Client) SendFileToRecipient(ctx context.Context, recipient string, filename string, data []byte) SendResult {
	return c.SendFileToRecipientType(ctx, "open_id", recipient, filename, data)
}

func (c *Client) SendFileToRecipientType(ctx context.Context, recipientType string, recipient string, filename string, data []byte) SendResult {
	recipientType = normalizeReceiveIDType(recipientType)
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return SendResult{}
	}
	if c.cfg.Mode != ModeApp {
		return SendResult{Err: fmt.Errorf("lark file delivery requires app mode")}
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

		fileKey, err := c.uploadFile(ctx, filename, data)
		if err != nil {
			if shouldRetryLarkError(err) {
				lastErr = fmt.Errorf("attempt %d upload file: %w", attempt, err)
				continue
			}
			return SendResult{Attempts: attempt, Err: err}
		}
		if err := c.postAppFileMessage(ctx, recipientType, recipient, fileKey); err != nil {
			if shouldRetryLarkError(err) {
				lastErr = fmt.Errorf("attempt %d send file message: %w", attempt, err)
				continue
			}
			return SendResult{Attempts: attempt, Err: err}
		}
		return SendResult{Attempts: attempt}
	}
	return SendResult{
		Attempts: maxAttempts,
		Err:      fmt.Errorf("lark app file delivery failed after %d attempts: %w", maxAttempts, lastErr),
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

type larkUploadFileResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		FileKey string `json:"file_key"`
	} `json:"data"`
}

type larkAPIError struct {
	Status int
	Code   int
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
		text = fmt.Sprintf("【%s】工單 %s\n%s", msg.Title, msg.TicketNo, msg.Body)
	}
	return text
}

func (c *Client) postAppMessage(ctx context.Context, receiveIDType, receiveID string, msg Message) (int, error) {
	accessToken, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return 0, err
	}

	msgType := "text"
	content, err := json.Marshal(larkContent{Text: c.buildText(msg)})
	if msg.Card != nil {
		msgType = "interactive"
		content, err = json.Marshal(BuildCardContent(*msg.Card))
	}
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(larkSendMessageRequest{
		ReceiveID: receiveID,
		MsgType:   msgType,
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
					Code:   parsed.Code,
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
			Code:   parsed.Code,
			Body:   fmt.Sprintf("lark send message http %d code=%d msg=%s", resp.StatusCode, parsed.Code, msg),
		}
	}
	if parsed.Code != 0 {
		return http.StatusBadRequest, &larkAPIError{
			Status: http.StatusBadRequest,
			Code:   parsed.Code,
			Body:   fmt.Sprintf("lark app error code %d: %s", parsed.Code, parsed.Msg),
		}
	}
	return resp.StatusCode, nil
}

func BuildCardContent(card Card) map[string]any {
	title := strings.TrimSpace(card.Title)
	if title == "" {
		title = "DBRE Maestro"
	}
	template := strings.TrimSpace(card.Template)
	if template == "" {
		template = "blue"
	}

	elements := make([]any, 0, 3)
	fieldLines := make([]string, 0, len(card.Fields))
	for _, field := range card.Fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		fieldLines = append(fieldLines, fmt.Sprintf("**%s：** %s", escapeLarkMarkdown(label), escapeLarkMarkdown(value)))
	}
	if len(fieldLines) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": strings.Join(fieldLines, "\n"),
		})
	}
	for _, block := range card.MarkdownBlocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": block,
		})
	}
	if len(card.Actions) > 0 {
		actionColumns := make([]any, 0, len(card.Actions))
		for _, action := range card.Actions {
			button := buildCardButton(action)
			if button == nil {
				continue
			}
			actionColumns = append(actionColumns, map[string]any{
				"tag":      "column",
				"width":    "auto",
				"elements": []any{button},
			})
		}
		if len(actionColumns) > 0 {
			elements = append(elements, map[string]any{
				"tag":                "column_set",
				"flex_mode":          "none",
				"horizontal_align":   "left",
				"horizontal_spacing": "8px",
				"columns":            actionColumns,
			})
		}
	}
	return map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"template": template,
			"title": map[string]string{
				"tag":     "plain_text",
				"content": title,
			},
		},
		"body": map[string]any{
			"elements": elements,
		},
	}
}

func buildCardButton(action CardAction) map[string]any {
	text := strings.TrimSpace(action.Text)
	if text == "" {
		return nil
	}
	button := map[string]any{
		"tag":  "button",
		"type": strings.TrimSpace(action.Type),
		"size": "medium",
		"text": map[string]string{
			"tag":     "plain_text",
			"content": text,
		},
	}
	if button["type"] == "" {
		button["type"] = "default"
	}
	behavior := map[string]any{}
	if action.URL != "" {
		behavior["type"] = "open_url"
		behavior["default_url"] = action.URL
	} else {
		value := map[string]any{}
		for key, item := range action.Value {
			value[key] = item
		}
		if action.Action != "" {
			value["action"] = action.Action
		}
		if len(value) == 0 {
			return nil
		}
		behavior["type"] = "callback"
		behavior["value"] = value
	}
	button["behaviors"] = []any{behavior}
	return button
}

func escapeLarkMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "*", "\\*")
	value = strings.ReplaceAll(value, "`", "\\`")
	return value
}

func (c *Client) uploadFile(ctx context.Context, filename string, data []byte) (string, error) {
	accessToken, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("file_type", "stream"); err != nil {
		return "", err
	}
	if err := writer.WriteField("file_name", filename); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.larksuite.com/open-apis/im/v1/files", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed larkUploadFileResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return "", fmt.Errorf("decode lark upload file response: %w", err)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Code != 0 {
		if parsed.Code == 99991663 {
			c.invalidateTenantAccessToken()
		}
		msg := strings.TrimSpace(parsed.Msg)
		if msg == "" {
			msg = strings.TrimSpace(string(respBody))
		}
		return "", &larkAPIError{
			Status: resp.StatusCode,
			Code:   parsed.Code,
			Body:   fmt.Sprintf("lark upload file http %d code=%d msg=%s", resp.StatusCode, parsed.Code, msg),
		}
	}
	if parsed.Data.FileKey == "" {
		return "", fmt.Errorf("lark upload file returned empty file_key")
	}
	return parsed.Data.FileKey, nil
}

func (c *Client) postAppFileMessage(ctx context.Context, receiveIDType, receiveID, fileKey string) error {
	content, err := json.Marshal(map[string]string{"file_key": fileKey})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(larkSendMessageRequest{
		ReceiveID: receiveID,
		MsgType:   "file",
		Content:   string(content),
	})
	if err != nil {
		return err
	}
	return c.postAppMessagePayload(ctx, receiveIDType, payload)
}

func (c *Client) postAppMessagePayload(ctx context.Context, receiveIDType string, payload []byte) error {
	accessToken, err := c.getTenantAccessToken(ctx)
	if err != nil {
		return err
	}
	endpoint := "https://open.larksuite.com/open-apis/im/v1/messages?receive_id_type=" + url.QueryEscape(receiveIDType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var parsed larkSendMessageResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &parsed); err != nil {
			return fmt.Errorf("decode lark message response: %w", err)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Code != 0 {
		if parsed.Code == 99991663 {
			c.invalidateTenantAccessToken()
		}
		msg := strings.TrimSpace(parsed.Msg)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return &larkAPIError{
			Status: resp.StatusCode,
			Code:   parsed.Code,
			Body:   fmt.Sprintf("lark send message http %d code=%d msg=%s", resp.StatusCode, parsed.Code, msg),
		}
	}
	return nil
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

func (c *Client) invalidateTenantAccessToken() {
	c.tokenMu.Lock()
	c.accessToken = ""
	c.tokenExpiry = time.Time{}
	c.tokenMu.Unlock()
}

func (e *larkAPIError) isInvalidAccessToken() bool {
	return e.Code == 99991663 || strings.Contains(strings.ToLower(e.Body), "invalid access token")
}

func shouldRetryLarkError(err error) bool {
	var apiErr *larkAPIError
	if errors.As(err, &apiErr) {
		return apiErr.isInvalidAccessToken() || apiErr.Status >= 500 || apiErr.Status == 0
	}
	return true
}
