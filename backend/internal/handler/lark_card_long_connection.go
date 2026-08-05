package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/dbre-maestro/maestro/internal/repository"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type LarkCardCallbackManager struct {
	settings *repository.SettingsRepo
	tickets  *TicketHandler

	mu        sync.Mutex
	activeKey string
	cancel    context.CancelFunc
	client    *larkws.Client
}

func NewLarkCardCallbackManager(settings *repository.SettingsRepo, tickets *TicketHandler) *LarkCardCallbackManager {
	return &LarkCardCallbackManager{settings: settings, tickets: tickets}
}

func (m *LarkCardCallbackManager) Reload(ctx context.Context) error {
	if m == nil || m.settings == nil || m.tickets == nil {
		return nil
	}
	settings, err := m.settings.Get(ctx)
	if err != nil {
		return fmt.Errorf("load lark callback settings: %w", err)
	}
	if settings == nil ||
		!settings.LarkInteractiveCardsEnabled ||
		settings.LarkCardCallbackMode != "long_connection" ||
		strings.TrimSpace(settings.LarkAppID) == "" ||
		!settings.LarkAppSecretConfigured {
		m.stop("settings_not_eligible")
		return nil
	}
	secret, err := m.settings.GetLarkAppSecret(ctx)
	if err != nil {
		return fmt.Errorf("load lark app secret: %w", err)
	}
	if strings.TrimSpace(secret) == "" {
		m.stop("missing_app_secret")
		return nil
	}

	appID := strings.TrimSpace(settings.LarkAppID)
	domain := larkCallbackDomainForSite(settings.LarkOAuthSite)
	key := larkCallbackRuntimeKey(appID, secret, domain)

	m.mu.Lock()
	if m.activeKey == key && m.client != nil {
		m.mu.Unlock()
		return nil
	}
	m.stopLocked("settings_changed")

	runCtx, cancel := context.WithCancel(context.Background())
	dispatcher := dispatcher.NewEventDispatcher("", "")
	dispatcher.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
		actionReq := larkCardActionRequestFromTrigger(event)
		if actionReq.Action == "" || actionReq.Ticket == "" {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "error", Content: "Invalid ticket action."}}, nil
		}
		resp, err := m.tickets.HandleLarkCardAction(ctx, actionReq)
		if err != nil {
			slog.Warn("lark card long connection action failed", "action", actionReq.Action, "ticket", actionReq.Ticket, "err", err)
		}
		return larkCardTriggerResponseFromMap(resp), nil
	})
	client := larkws.NewClient(
		appID,
		secret,
		larkws.WithDomain(domain),
		larkws.WithEventHandler(dispatcher),
		larkws.WithOnReady(func() {
			slog.Info("lark card long connection ready", "app_id", appID, "domain", domain)
		}),
		larkws.WithOnError(func(err error) {
			slog.Warn("lark card long connection error", "app_id", appID, "domain", domain, "err", err)
		}),
		larkws.WithOnReconnecting(func() {
			slog.Info("lark card long connection reconnecting", "app_id", appID)
		}),
		larkws.WithOnReconnected(func() {
			slog.Info("lark card long connection reconnected", "app_id", appID)
		}),
		larkws.WithOnDisconnected(func() {
			slog.Info("lark card long connection disconnected", "app_id", appID)
		}),
	)
	m.activeKey = key
	m.cancel = cancel
	m.client = client
	m.mu.Unlock()

	go func() {
		if err := client.Start(runCtx); err != nil && runCtx.Err() == nil {
			slog.Warn("lark card long connection stopped with error", "app_id", appID, "domain", domain, "err", err)
		}
	}()
	slog.Info("lark card long connection started", "app_id", appID, "domain", domain)
	return nil
}

func (m *LarkCardCallbackManager) Stop() {
	if m == nil {
		return
	}
	m.stop("shutdown")
}

func (m *LarkCardCallbackManager) stop(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked(reason)
}

func (m *LarkCardCallbackManager) stopLocked(reason string) {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.client != nil {
		m.client.Close()
		m.client = nil
		slog.Info("lark card long connection stopped", "reason", reason)
	}
	m.activeKey = ""
}

func larkCallbackRuntimeKey(appID string, secret string, domain string) string {
	sum := sha256.Sum256([]byte(appID + "\x00" + secret + "\x00" + domain))
	return strings.TrimSpace(appID) + ":" + hex.EncodeToString(sum[:])
}

func larkCallbackDomainForSite(site string) string {
	if strings.EqualFold(strings.TrimSpace(site), "feishu") {
		return "https://open.feishu.cn"
	}
	return "https://open.larksuite.com"
}

func larkCardActionRequestFromTrigger(event *callback.CardActionTriggerEvent) larkCardActionRequest {
	if event == nil || event.Event == nil {
		return larkCardActionRequest{}
	}
	action := map[string]any{}
	if event.Event.Action != nil {
		action = event.Event.Action.Value
	}
	operatorOpenID := ""
	if event.Event.Operator != nil {
		operatorOpenID = strings.TrimSpace(event.Event.Operator.OpenID)
	}
	return larkCardActionRequest{
		Action: stringFromAny(action["action"]),
		Ticket: firstNonEmptyString(
			stringFromAny(action["ticket_no"]),
			stringFromAny(action["ticket_id"]),
		),
		OpenID:    operatorOpenID,
		UnionID:   larkCardActionUnionIDFromRaw(event),
		CardStage: stringFromAny(action["card_stage"]),
		Handled:   boolFromAny(action["handled"]),
	}
}

func larkCardActionUnionIDFromRaw(event *callback.CardActionTriggerEvent) string {
	if event == nil || event.EventReq == nil || len(event.EventReq.Body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(event.EventReq.Body, &payload); err != nil {
		return ""
	}
	return stringFromAny(mapValue(payload, "event", "operator", "union_id"))
}

func larkCardTriggerResponseFromMap(resp map[string]any) *callback.CardActionTriggerResponse {
	if resp == nil {
		return nil
	}
	out := &callback.CardActionTriggerResponse{}
	if toast, ok := resp["toast"].(map[string]string); ok {
		out.Toast = &callback.Toast{
			Type:    strings.TrimSpace(toast["type"]),
			Content: strings.TrimSpace(toast["content"]),
		}
	} else if toastAny, ok := resp["toast"].(map[string]any); ok {
		out.Toast = &callback.Toast{
			Type:    stringFromAny(toastAny["type"]),
			Content: stringFromAny(toastAny["content"]),
		}
	}
	if card, ok := resp["card"]; ok && card != nil {
		out.Card = &callback.Card{
			Type: "raw",
			Data: card,
		}
	}
	return out
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(uint64(typed)) {
			return fmt.Sprintf("%d", uint64(typed))
		}
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	}
	return ""
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	}
	return false
}

func mapValue(values map[string]any, path ...string) any {
	var current any = values
	for _, key := range path {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}
