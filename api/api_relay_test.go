package center

import (
	"testing"
)

func TestDataRelayStructure(t *testing.T) {
	relay := DataRelay{
		ID:         "relay-tokyo-1",
		Name:       "Tokyo Relay",
		Ipv4:       "203.0.113.10",
		Ipv6:       "2001:db8::1",
		HopPortMin: 20000,
		HopPortMax: 50000,
		Region:     "ap-northeast-1",
	}

	if relay.ID != "relay-tokyo-1" {
		t.Errorf("Expected ID to be relay-tokyo-1, got %s", relay.ID)
	}
	if relay.HopPortMin != 20000 {
		t.Errorf("Expected HopPortMin to be 20000, got %d", relay.HopPortMin)
	}
}

func TestDataRelayListResponse(t *testing.T) {
	response := DataRelayListResponse{
		Relays: []DataRelay{
			{ID: "relay-1", Name: "Relay 1"},
			{ID: "relay-2", Name: "Relay 2"},
		},
	}

	if len(response.Relays) != 2 {
		t.Errorf("Expected 2 relays, got %d", len(response.Relays))
	}
}
