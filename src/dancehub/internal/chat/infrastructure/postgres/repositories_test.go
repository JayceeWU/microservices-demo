package postgres

import (
	"encoding/json"
	"testing"
)

func TestRealtimeEnvelopeAlwaysCarriesConversationID(t *testing.T) {
	t.Parallel()
	for _, eventType := range []string{"message_updated", "message_withdrawn", "read_updated"} {
		raw, err := realtimeEnvelope("conv-1", eventType, map[string]any{"message_id": "msg-1"})
		if err != nil {
			t.Fatalf("%s: %v", eventType, err)
		}
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("%s: %v", eventType, err)
		}
		if event["conversation_id"] != "conv-1" || event["event_type"] != eventType || event["message_id"] != "msg-1" {
			t.Fatalf("%s: unexpected envelope %v", eventType, event)
		}
	}
}

func TestRealtimeEnvelopeAcceptsNilPayload(t *testing.T) {
	t.Parallel()
	raw, err := realtimeEnvelope("conv-1", "presence", nil)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event["conversation_id"] != "conv-1" {
		t.Fatalf("unexpected envelope %v", event)
	}
}
