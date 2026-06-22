package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/dbre-maestro/maestro/internal/repository"
)

type Dispatcher struct {
	settings     *repository.SettingsRepo
	users        *repository.UserRepo
	webhookURL   string
	mu           sync.Mutex
	cachedKey    string
	cachedClient *Client
}

func NewDispatcher(settings *repository.SettingsRepo, users *repository.UserRepo, webhookURL string) *Dispatcher {
	return &Dispatcher{
		settings:   settings,
		users:      users,
		webhookURL: strings.TrimSpace(webhookURL),
	}
}

func (d *Dispatcher) NotifyUsers(ctx context.Context, userIDs []uint64, msg Message) SendResult {
	client, mode, err := d.resolveClient(ctx)
	if err != nil {
		return SendResult{Err: err}
	}
	if client == nil {
		return SendResult{SkippedReason: "lark_not_configured"}
	}
	if mode == ModeWebhook {
		return client.Send(ctx, msg)
	}

	users, err := d.users.ListByIDs(ctx, dedupeUserIDs(userIDs))
	if err != nil {
		return SendResult{Err: fmt.Errorf("load lark recipients failed: %w", err)}
	}

	recipients := make([]string, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		recipient := strings.TrimSpace(user.LarkRecipient)
		if recipient == "" {
			continue
		}
		key := strings.ToLower(recipient)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		recipients = append(recipients, recipient)
	}
	if len(recipients) == 0 {
		return SendResult{SkippedReason: "no_lark_recipient_open_id"}
	}

	var failed []string
	totalAttempts := 0
	for _, recipient := range recipients {
		result := client.SendToRecipient(ctx, recipient, msg)
		totalAttempts += result.Attempts
		if result.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", recipient, result.Err.Error()))
		}
	}
	if len(failed) > 0 {
		return SendResult{
			Attempts: totalAttempts,
			Err:      fmt.Errorf("lark notify failed for %d recipient(s): %s", len(failed), strings.Join(failed, "; ")),
		}
	}
	return SendResult{Attempts: totalAttempts}
}

func (d *Dispatcher) resolveClient(ctx context.Context) (*Client, Mode, error) {
	if d.settings != nil {
		settings, err := d.settings.Get(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("load lark settings failed: %w", err)
		}
		if settings != nil {
			appID := strings.TrimSpace(settings.LarkAppID)
			if appID != "" {
				secret, err := d.settings.GetLarkAppSecret(ctx)
				if err != nil {
					return nil, "", err
				}
				secret = strings.TrimSpace(secret)
				if secret != "" {
					cfg := Config{
						Mode:      ModeApp,
						AppID:     appID,
						AppSecret: secret,
					}
					return d.getOrCreateClient(cfg), ModeApp, nil
				}
			}
		}
	}

	if d.webhookURL == "" {
		return nil, "", nil
	}
	cfg := Config{
		Mode:       ModeWebhook,
		WebhookURL: d.webhookURL,
	}
	return d.getOrCreateClient(cfg), ModeWebhook, nil
}

func (d *Dispatcher) getOrCreateClient(cfg Config) *Client {
	cacheKey := string(cfg.Mode) + "|" + cfg.WebhookURL + "|" + cfg.AppID + "|" + cfg.AppSecret

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cachedClient != nil && d.cachedKey == cacheKey {
		return d.cachedClient
	}

	d.cachedKey = cacheKey
	d.cachedClient = NewClient(cfg)
	return d.cachedClient
}

func dedupeUserIDs(userIDs []uint64) []uint64 {
	if len(userIDs) == 0 {
		return []uint64{}
	}
	seen := make(map[uint64]struct{}, len(userIDs))
	unique := make([]uint64, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}
	return unique
}
