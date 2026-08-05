package handler

import (
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestLarkCardActionRequestFromTriggerIncludesUnionIDFromRawPayload(t *testing.T) {
	event := &callback.CardActionTriggerEvent{
		EventReq: &larkevent.EventReq{Body: []byte(`{
			"event": {
				"operator": {
					"open_id": "ou_actor",
					"union_id": "on_actor"
				}
			}
		}`)},
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "ou_actor"},
			Action: &callback.CallBackAction{Value: map[string]any{
				"action":     larkTicketActionApprove,
				"ticket_no":  "TK-1",
				"card_stage": larkTicketCardStageReview,
				"handled":    true,
			}},
		},
	}

	got := larkCardActionRequestFromTrigger(event)
	if got.Action != larkTicketActionApprove || got.Ticket != "TK-1" || got.OpenID != "ou_actor" || got.UnionID != "on_actor" || got.CardStage != larkTicketCardStageReview || !got.Handled {
		t.Fatalf("larkCardActionRequestFromTrigger() = %#v", got)
	}
}

func TestLarkCardTriggerResponseFromMapIncludesToastAndRawCard(t *testing.T) {
	resp := larkCardTriggerResponseFromMap(map[string]any{
		"toast": map[string]string{"type": "success", "content": "ok"},
		"card":  map[string]any{"schema": "2.0"},
	})
	if resp == nil || resp.Toast == nil || resp.Toast.Type != "success" || resp.Toast.Content != "ok" {
		t.Fatalf("toast response = %#v", resp)
	}
	if resp.Card == nil || resp.Card.Type != "raw" || resp.Card.Data == nil {
		t.Fatalf("card response = %#v", resp)
	}
}

func TestLarkCallbackDomainForSite(t *testing.T) {
	tests := []struct {
		name string
		site string
		want string
	}{
		{name: "lark", site: "lark", want: "https://open.larksuite.com"},
		{name: "feishu", site: "feishu", want: "https://open.feishu.cn"},
		{name: "default", site: "", want: "https://open.larksuite.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := larkCallbackDomainForSite(tt.site); got != tt.want {
				t.Fatalf("larkCallbackDomainForSite(%q) = %q, want %q", tt.site, got, tt.want)
			}
		})
	}
}
