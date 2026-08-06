package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/dbre-maestro/maestro/internal/model"
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

type RecipientSendResult struct {
	UserID        uint64
	Attempts      int
	Err           error
	SkippedReason string
}

type BatchSendResult struct {
	Attempts      int
	Err           error
	SkippedReason string
	Deliveries    []RecipientSendResult
}

func NewDispatcher(settings *repository.SettingsRepo, users *repository.UserRepo, webhookURL string) *Dispatcher {
	return &Dispatcher{
		settings:   settings,
		users:      users,
		webhookURL: strings.TrimSpace(webhookURL),
	}
}

func (d *Dispatcher) NotifyUsers(ctx context.Context, userIDs []uint64, msg Message) BatchSendResult {
	uniqueUserIDs := dedupeUserIDs(userIDs)
	client, mode, err := d.resolveClient(ctx)
	if err != nil {
		return BatchSendResult{Err: err}
	}
	if client == nil {
		return skippedBatchSendResult(uniqueUserIDs, "lark_not_configured")
	}
	if mode == ModeWebhook {
		result := client.Send(ctx, msg)
		return batchSendResultForUsers(uniqueUserIDs, result)
	}

	users, err := d.users.ListByIDs(ctx, uniqueUserIDs)
	if err != nil {
		return BatchSendResult{Err: fmt.Errorf("load lark recipients failed: %w", err)}
	}

	usersByID := make(map[uint64]model.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}

	deliveries := make([]RecipientSendResult, 0, len(uniqueUserIDs))
	recipients := make(map[string]larkRecipient, len(users))
	usersByRecipient := make(map[string][]uint64, len(users))
	for _, userID := range uniqueUserIDs {
		user, ok := usersByID[userID]
		if !ok {
			deliveries = append(deliveries, RecipientSendResult{UserID: userID, SkippedReason: "user_not_found"})
			continue
		}
		recipientType, recipient := larkRecipientForUser(user)
		if recipient == "" {
			deliveries = append(deliveries, RecipientSendResult{UserID: userID, SkippedReason: "no_lark_recipient"})
			continue
		}
		key := strings.ToLower(recipientType + ":" + recipient)
		if _, ok := recipients[key]; !ok {
			recipients[key] = larkRecipient{recipientType: recipientType, recipient: recipient}
		}
		usersByRecipient[key] = append(usersByRecipient[key], userID)
	}
	if len(recipients) == 0 {
		return BatchSendResult{
			SkippedReason: "no_lark_recipient",
			Deliveries:    deliveries,
		}
	}

	var failed []string
	totalAttempts := 0
	for key, recipient := range recipients {
		result := client.SendToRecipientType(ctx, recipient.recipientType, recipient.recipient, msg)
		totalAttempts += result.Attempts
		for _, userID := range usersByRecipient[key] {
			deliveries = append(deliveries, RecipientSendResult{
				UserID:        userID,
				Attempts:      result.Attempts,
				Err:           result.Err,
				SkippedReason: result.SkippedReason,
			})
		}
		if result.Err != nil {
			failed = append(failed, fmt.Sprintf("%s:%s: %s", recipient.recipientType, recipient.recipient, result.Err.Error()))
		}
	}
	if len(failed) > 0 {
		return BatchSendResult{
			Attempts:   totalAttempts,
			Err:        fmt.Errorf("lark notify failed for %d recipient(s): %s", len(failed), strings.Join(failed, "; ")),
			Deliveries: deliveries,
		}
	}
	return BatchSendResult{Attempts: totalAttempts, Deliveries: deliveries}
}

func (d *Dispatcher) SendFileToUsers(ctx context.Context, userIDs []uint64, filename string, data []byte) SendResult {
	client, mode, err := d.resolveClient(ctx)
	if err != nil {
		return SendResult{Err: err}
	}
	if client == nil {
		return SendResult{SkippedReason: "lark_not_configured"}
	}
	if mode != ModeApp {
		return SendResult{Err: fmt.Errorf("lark file delivery requires app mode")}
	}

	users, err := d.users.ListByIDs(ctx, dedupeUserIDs(userIDs))
	if err != nil {
		return SendResult{Err: fmt.Errorf("load lark recipients failed: %w", err)}
	}
	recipients := make([]larkRecipient, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		recipientType, recipient := larkRecipientForUser(user)
		if recipient == "" {
			continue
		}
		key := strings.ToLower(recipientType + ":" + recipient)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		recipients = append(recipients, larkRecipient{recipientType: recipientType, recipient: recipient})
	}
	if len(recipients) == 0 {
		return SendResult{SkippedReason: "no_lark_recipient"}
	}

	var failed []string
	totalAttempts := 0
	for _, recipient := range recipients {
		result := client.SendFileToRecipientType(ctx, recipient.recipientType, recipient.recipient, filename, data)
		totalAttempts += result.Attempts
		if result.Err != nil {
			failed = append(failed, fmt.Sprintf("%s:%s: %s", recipient.recipientType, recipient.recipient, result.Err.Error()))
		}
	}
	if len(failed) > 0 {
		return SendResult{
			Attempts: totalAttempts,
			Err:      fmt.Errorf("lark file delivery failed for %d recipient(s): %s", len(failed), strings.Join(failed, "; ")),
		}
	}
	return SendResult{Attempts: totalAttempts}
}

type larkRecipient struct {
	recipientType string
	recipient     string
}

func larkRecipientForUser(user model.User) (string, string) {
	if strings.TrimSpace(user.LarkRecipientType) == "union_id" {
		return "union_id", strings.TrimSpace(user.LarkUnionID)
	}
	return "open_id", strings.TrimSpace(user.LarkRecipient)
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

func batchSendResultForUsers(userIDs []uint64, result SendResult) BatchSendResult {
	deliveries := make([]RecipientSendResult, 0, len(userIDs))
	for _, userID := range userIDs {
		deliveries = append(deliveries, RecipientSendResult{
			UserID:        userID,
			Attempts:      result.Attempts,
			Err:           result.Err,
			SkippedReason: result.SkippedReason,
		})
	}
	return BatchSendResult{
		Attempts:      result.Attempts,
		Err:           result.Err,
		SkippedReason: result.SkippedReason,
		Deliveries:    deliveries,
	}
}

func skippedBatchSendResult(userIDs []uint64, reason string) BatchSendResult {
	deliveries := make([]RecipientSendResult, 0, len(userIDs))
	for _, userID := range userIDs {
		deliveries = append(deliveries, RecipientSendResult{UserID: userID, SkippedReason: reason})
	}
	return BatchSendResult{
		SkippedReason: reason,
		Deliveries:    deliveries,
	}
}
