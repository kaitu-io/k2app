package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestApiK2Relays_TrialUserSeesOnlyTrialPool —— /api/relays 下发节点 IP，同样受池限制。
// Needs dev MySQL.
func TestApiK2Relays_TrialUserSeesOnlyTrialPool(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	_, regular, trial := seedPoolFixture(t)

	body := callHandlerAs(t, api_k2_relays, poolTrialUser(), "/api/relays", nil)
	assert.Equal(t, 0, countPresentIP(body, regular...), "trial user must not receive regular-pool relay IPs")
	assert.Equal(t, 1, countPresentIP(body, trial), "trial user must receive the trial-pool relay")

	body = callHandlerAs(t, api_k2_relays, poolPaidUser(), "/api/relays", nil)
	assert.Equal(t, 3, countPresentIP(body, append(regular, trial)...), "paid user sees all pools")
}
