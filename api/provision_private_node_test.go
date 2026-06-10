package center

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderProvisionUserData(t *testing.T) {
	ud := renderProvisionUserData(provisionParams{
		NodeSecret: "node-secret-xyz",
		ClaimToken: "claim-abc",
		CenterURL:  "https://k2.example.com",
		Domain:     "*.1-2-3-4.sslip.io",
	})
	require.NotEmpty(t, ud)
	assert.Contains(t, ud, "K2_NODE_SECRET=node-secret-xyz")
	assert.Contains(t, ud, "K2_PRIVATE_CLAIM=claim-abc")
	assert.Contains(t, ud, "K2_CENTER_URL=https://k2.example.com")
	assert.Contains(t, ud, "K2_DOMAIN=*.1-2-3-4.sslip.io")
	assert.True(t, strings.HasPrefix(ud, "#!") || strings.HasPrefix(ud, "#cloud-config"),
		"user-data must be a runnable bootstrap")
}
