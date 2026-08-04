package handler

import "testing"

func TestParseLarkCardActionSupportsEventPayload(t *testing.T) {
	got := parseLarkCardAction(map[string]any{
		"event": map[string]any{
			"operator": map[string]any{
				"open_id":  "ou_x",
				"union_id": "on_y",
			},
			"action": map[string]any{
				"value": map[string]any{
					"action":    larkTicketActionApprove,
					"ticket_no": "TK-1",
				},
			},
		},
	})
	if got.Action != larkTicketActionApprove || got.Ticket != "TK-1" || got.OpenID != "ou_x" || got.UnionID != "on_y" {
		t.Fatalf("parseLarkCardAction() = %#v", got)
	}
}

func TestParseLarkCardActionSupportsLegacyPayload(t *testing.T) {
	got := parseLarkCardAction(map[string]any{
		"open_id": "ou_legacy",
		"action": map[string]any{
			"value": map[string]any{
				"action":    larkTicketActionReject,
				"ticket_id": float64(42),
			},
		},
	})
	if got.Action != larkTicketActionReject || got.Ticket != "42" || got.OpenID != "ou_legacy" {
		t.Fatalf("parseLarkCardAction() = %#v", got)
	}
}

func TestParseLarkCardActionSupportsViewDetails(t *testing.T) {
	got := parseLarkCardAction(map[string]any{
		"event": map[string]any{
			"operator": map[string]any{"union_id": "on_viewer"},
			"action": map[string]any{
				"value": map[string]any{
					"action":    larkTicketActionViewDetails,
					"ticket_no": "TK-VIEW",
				},
			},
		},
	})
	if got.Action != larkTicketActionViewDetails || got.Ticket != "TK-VIEW" || got.UnionID != "on_viewer" {
		t.Fatalf("parseLarkCardAction() = %#v", got)
	}
}
