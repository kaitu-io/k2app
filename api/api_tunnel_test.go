package center

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTunnelProtocolK2V5_Constant verifies the k2v5 protocol constant is defined
// with the correct string value. The constant is required for DB storage and
// protocol-level comparisons in the tunnel query handler.
func TestTunnelProtocolK2V5_Constant(t *testing.T) {
	t.Run("k2v5 constant has correct string value", func(t *testing.T) {
		assert.Equal(t, TunnelProtocol("k2v5"), TunnelProtocolK2V5,
			"TunnelProtocolK2V5 must equal the string 'k2v5'")
	})

	t.Run("k2v5 constant is distinct from k2v4", func(t *testing.T) {
		assert.NotEqual(t, TunnelProtocolK2V4, TunnelProtocolK2V5,
			"TunnelProtocolK2V5 must be distinct from TunnelProtocolK2V4")
	})

	t.Run("k2v5 constant is distinct from k2oc", func(t *testing.T) {
		assert.NotEqual(t, TunnelProtocolK2OC, TunnelProtocolK2V5,
			"TunnelProtocolK2V5 must be distinct from TunnelProtocolK2OC")
	})

	t.Run("k2v5 protocol can be stored in SlaveTunnel struct", func(t *testing.T) {
		tunnel := SlaveTunnel{Protocol: TunnelProtocolK2V5}
		require.Equal(t, TunnelProtocolK2V5, tunnel.Protocol,
			"SlaveTunnel.Protocol must accept TunnelProtocolK2V5")
	})
}

// TestTunnelProtocolK2V5_BackwardCompatibility verifies the backward compatibility
// logic for k2v4 clients that should also receive k2v5 tunnels.
//
// k2v5 nodes physically serve k2v4 connections via front-door forwarding:
// the k2v5 front-door detects non-ECH traffic and forwards it to a k2v4-slave.
// Therefore, a client requesting k2v4 tunnels should receive both k2v4 and k2v5
// tunnels; a client requesting k2v5 tunnels should receive only k2v5 tunnels.
func TestTunnelProtocolK2V5_BackwardCompatibility(t *testing.T) {
	t.Run("k2v4 request protocol set includes k2v5", func(t *testing.T) {
		// When a client requests k2v4 tunnels, the query set must include k2v5.
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2V4)
		assert.Contains(t, protocols, TunnelProtocolK2V4,
			"k2v4 query set must contain k2v4")
		assert.Contains(t, protocols, TunnelProtocolK2V5,
			"k2v4 query set must include k2v5 for backward compatibility")
	})

	t.Run("k2v5 request protocol set contains only k2v5", func(t *testing.T) {
		// When a client requests k2v5 tunnels, only k2v5 tunnels are returned.
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2V5)
		assert.Equal(t, []TunnelProtocol{TunnelProtocolK2V5}, protocols,
			"k2v5 query set must contain only k2v5")
	})

	t.Run("k2oc request protocol set contains only k2oc", func(t *testing.T) {
		// k2oc is an unrelated protocol — its set must not bleed into k2v4/k2v5.
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2OC)
		assert.Equal(t, []TunnelProtocol{TunnelProtocolK2OC}, protocols,
			"k2oc query set must contain only k2oc")
	})

	t.Run("k2wss request protocol set contains only k2wss", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2WSS)
		assert.Equal(t, []TunnelProtocol{TunnelProtocolK2WSS}, protocols,
			"k2wss query set must contain only k2wss")
	})

	t.Run("k2v4 query set does not include k2oc", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2V4)
		assert.NotContains(t, protocols, TunnelProtocolK2OC,
			"k2v4 query set must not contain k2oc")
	})
}

// TestDataSlaveTunnelListResponse_ECHConfigListField tests that the response type
// has an ECHConfigList field for K2v4 connections.
//
// This is critical for K2v4 protocol: ECH (Encrypted Client Hello) allows
// clients to encrypt the real SNI while using a camouflage SNI on the wire,
// preventing the server's SNI router from rejecting the connection.
func TestDataSlaveTunnelListResponse_ECHConfigListField(t *testing.T) {
	t.Run("response struct should have ECHConfigList field", func(t *testing.T) {
		// Create a response with ECH config list
		echConfigList := base64.StdEncoding.EncodeToString([]byte("test-ech-config"))
		resp := DataSlaveTunnelListResponse{
			Items:         []DataSlaveTunnel{},
			ECHConfigList: echConfigList,
		}

		// Serialize to JSON
		jsonBytes, err := json.Marshal(resp)
		require.NoError(t, err)

		// Deserialize back
		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify echConfigList field exists in JSON
		_, exists := parsed["echConfigList"]
		assert.True(t, exists, "JSON should contain 'echConfigList' field")
		assert.Equal(t, echConfigList, parsed["echConfigList"], "echConfigList value should match")
	})

	t.Run("echConfigList should be omitted when empty", func(t *testing.T) {
		// Create a response without ECH config list
		resp := DataSlaveTunnelListResponse{
			Items:         []DataSlaveTunnel{},
			ECHConfigList: "", // empty
		}

		// Serialize to JSON
		jsonBytes, err := json.Marshal(resp)
		require.NoError(t, err)

		// Deserialize back
		var parsed map[string]any
		err = json.Unmarshal(jsonBytes, &parsed)
		require.NoError(t, err)

		// Verify echConfigList field is omitted when empty (due to omitempty tag)
		_, exists := parsed["echConfigList"]
		assert.False(t, exists, "JSON should omit 'echConfigList' when empty")
	})
}

// TestBuildECHConfigList tests the ECHConfigList builder function
func TestBuildECHConfigList(t *testing.T) {
	t.Run("builds valid ECHConfigList from config", func(t *testing.T) {
		// Create a mock ECH config (just bytes for testing structure)
		mockECHConfig := []byte{0xfe, 0x0d, 0x00, 0x10, 0x01} // version + length prefix + config_id

		echConfigList := buildECHConfigList([][]byte{mockECHConfig})

		// ECHConfigList format: 2-byte length prefix + configs
		assert.GreaterOrEqual(t, len(echConfigList), 2, "should have length prefix")

		// First 2 bytes are the total length
		totalLen := int(echConfigList[0])<<8 | int(echConfigList[1])
		assert.Equal(t, len(mockECHConfig), totalLen, "length prefix should match config length")

		// Verify the config is included after the length prefix
		assert.Equal(t, mockECHConfig, echConfigList[2:], "config should follow length prefix")
	})

	t.Run("builds ECHConfigList with multiple configs", func(t *testing.T) {
		config1 := []byte{0x01, 0x02, 0x03}
		config2 := []byte{0x04, 0x05}

		echConfigList := buildECHConfigList([][]byte{config1, config2})

		// Length prefix should be sum of both configs
		expectedLen := len(config1) + len(config2)
		actualLen := int(echConfigList[0])<<8 | int(echConfigList[1])
		assert.Equal(t, expectedLen, actualLen)
	})

	t.Run("result is valid base64 encodable", func(t *testing.T) {
		mockConfig := []byte{0xfe, 0x0d, 0x00, 0x10}
		echConfigList := buildECHConfigList([][]byte{mockConfig})

		// Should be able to base64 encode without error
		encoded := base64.StdEncoding.EncodeToString(echConfigList)
		assert.NotEmpty(t, encoded)

		// Should decode back to same bytes
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		assert.NoError(t, err)
		assert.Equal(t, echConfigList, decoded)
	})
}
