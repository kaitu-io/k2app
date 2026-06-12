package sidecar

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNodeUpsertRequest_PrivateClaimSerialization verifies the private-node
// claim token marshals under the exact wire key Center expects ("privateClaim")
// and is omitted entirely when empty (zero behavior change for shared nodes).
func TestNodeUpsertRequest_PrivateClaimSerialization(t *testing.T) {
	t.Run("present when set", func(t *testing.T) {
		req := NodeUpsertRequest{
			Country:      "US",
			Name:         "1.2.3.4",
			PrivateClaim: "tok123",
		}
		b, err := json.Marshal(req)
		require.NoError(t, err)

		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(b, &m))
		assert.Equal(t, "tok123", m["privateClaim"],
			"privateClaim must serialize under the exact wire key Center expects")
		assert.Contains(t, string(b), `"privateClaim":"tok123"`)
	})

	t.Run("omitted when empty (shared node)", func(t *testing.T) {
		req := NodeUpsertRequest{
			Country: "US",
			Name:    "1.2.3.4",
			// PrivateClaim left empty
		}
		b, err := json.Marshal(req)
		require.NoError(t, err)

		var m map[string]interface{}
		require.NoError(t, json.Unmarshal(b, &m))
		_, present := m["privateClaim"]
		assert.False(t, present,
			"privateClaim must be omitted from JSON when empty (omitempty)")
		assert.False(t, strings.Contains(string(b), "privateClaim"),
			"shared-node request must be byte-identical to today (no privateClaim key)")
	})
}

// TestNode_buildNodeUpsertRequest_CarriesPrivateClaim verifies Register()'s
// request builder copies the Node's PrivateClaim into the upsert request.
func TestNode_buildNodeUpsertRequest_CarriesPrivateClaim(t *testing.T) {
	t.Run("private node carries claim", func(t *testing.T) {
		n := &Node{
			IPv4:         "1.2.3.4",
			Country:      "US",
			Region:       "us-west",
			Name:         "node-a",
			Secret:       "sekret",
			PrivateClaim: "claim-abc",
		}
		req := n.buildNodeUpsertRequest(nil)
		assert.Equal(t, "claim-abc", req.PrivateClaim)
		assert.Equal(t, "US", req.Country)
		assert.Equal(t, "sekret", req.SecretToken)
	})

	t.Run("shared node has empty claim", func(t *testing.T) {
		n := &Node{
			IPv4:    "5.6.7.8",
			Country: "JP",
			Name:    "node-b",
			Secret:  "sekret",
		}
		req := n.buildNodeUpsertRequest(nil)
		assert.Equal(t, "", req.PrivateClaim)

		b, err := json.Marshal(req)
		require.NoError(t, err)
		assert.False(t, strings.Contains(string(b), "privateClaim"))
	})
}
