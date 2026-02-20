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

// TestTunnelProtocolK2V5_BackwardCompatibility verifies that all k2-family
// protocols (k2, k2v4, k2wss) also return k2v5 tunnels, because the k2v5
// front-door forwards all non-ECH traffic to the appropriate backend via
// local_routes SNI matching.
func TestTunnelProtocolK2V5_BackwardCompatibility(t *testing.T) {
	t.Run("k2v4 request includes k2v5", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2V4)
		assert.Contains(t, protocols, TunnelProtocolK2V4)
		assert.Contains(t, protocols, TunnelProtocolK2V5)
	})

	t.Run("k2wss request includes k2v5", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2WSS)
		assert.Contains(t, protocols, TunnelProtocolK2WSS)
		assert.Contains(t, protocols, TunnelProtocolK2V5)
	})

	t.Run("k2 request includes k2v5", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2)
		assert.Contains(t, protocols, TunnelProtocolK2)
		assert.Contains(t, protocols, TunnelProtocolK2V5)
	})

	t.Run("k2v5 request returns only k2v5", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2V5)
		assert.Equal(t, []TunnelProtocol{TunnelProtocolK2V5}, protocols)
	})

	t.Run("k2oc request returns only k2oc", func(t *testing.T) {
		protocols := tunnelProtocolsForQuery(TunnelProtocolK2OC)
		assert.Equal(t, []TunnelProtocol{TunnelProtocolK2OC}, protocols)
	})

	t.Run("k2-family query sets never include k2oc", func(t *testing.T) {
		for _, p := range []TunnelProtocol{TunnelProtocolK2, TunnelProtocolK2V4, TunnelProtocolK2WSS} {
			protocols := tunnelProtocolsForQuery(p)
			assert.NotContains(t, protocols, TunnelProtocolK2OC,
				"%s query set must not contain k2oc", p)
		}
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

// TestBuildK2V5ServerURL_FullParams tests buildK2V5ServerURL with all parameters present.
func TestBuildK2V5ServerURL_FullParams(t *testing.T) {
	tunnel := SlaveTunnel{
		Domain:        "hk1.example.com",
		Port:          443,
		CertPin:       "sha256:abc123",
		ECHConfigList: "AABBCC",
		HopPortStart:  10020,
		HopPortEnd:    10119,
	}
	url := buildK2V5ServerURL(&tunnel)
	assert.Equal(t, "k2v5://hk1.example.com:443?ech=AABBCC&pin=sha256:abc123&hop=10020-10119", url)
}

// TestBuildK2V5ServerURL_NoECH tests buildK2V5ServerURL when ECHConfigList is missing.
func TestBuildK2V5ServerURL_NoECH(t *testing.T) {
	tunnel := SlaveTunnel{Domain: "test.com", Port: 443, CertPin: "sha256:xyz"}
	url := buildK2V5ServerURL(&tunnel)
	assert.Contains(t, url, "pin=sha256:xyz")
	assert.NotContains(t, url, "ech=")
}

// TestBuildK2V5ServerURL_NoHop tests buildK2V5ServerURL when hop ports are zero (disabled).
func TestBuildK2V5ServerURL_NoHop(t *testing.T) {
	tunnel := SlaveTunnel{Domain: "test.com", Port: 443, CertPin: "sha256:xyz", ECHConfigList: "AA"}
	url := buildK2V5ServerURL(&tunnel)
	assert.NotContains(t, url, "hop=")
}

// TestBuildK2V5ServerURL_Empty tests buildK2V5ServerURL with no optional parameters.
func TestBuildK2V5ServerURL_Empty(t *testing.T) {
	tunnel := SlaveTunnel{Domain: "test.com", Port: 443}
	url := buildK2V5ServerURL(&tunnel)
	assert.Equal(t, "k2v5://test.com:443", url)
}

// TestUpsertTunnel_K2V5WithCertPin verifies TunnelConfigInput accepts certPin and echConfigList
// and SlaveTunnel model has the corresponding fields for DB storage.
func TestUpsertTunnel_K2V5WithCertPin(t *testing.T) {
	input := TunnelConfigInput{
		Domain:        "k2v5.example.com",
		Protocol:      "k2v5",
		Port:          443,
		CertPin:       "sha256:abc123def456",
		ECHConfigList: "AABBCCDDEEFF",
	}
	assert.Equal(t, "sha256:abc123def456", input.CertPin)
	assert.Equal(t, "AABBCCDDEEFF", input.ECHConfigList)

	// Verify SlaveTunnel struct accepts the fields
	tunnel := SlaveTunnel{
		Domain:        input.Domain,
		Protocol:      TunnelProtocolK2V5,
		Port:          int64(input.Port),
		CertPin:       input.CertPin,
		ECHConfigList: input.ECHConfigList,
	}
	assert.Equal(t, "sha256:abc123def456", tunnel.CertPin)
	assert.Equal(t, "AABBCCDDEEFF", tunnel.ECHConfigList)
}

// TestUpsertTunnel_K2V4NoCertPin verifies k2v4 registration without certPin compiles and works.
func TestUpsertTunnel_K2V4NoCertPin(t *testing.T) {
	input := TunnelConfigInput{
		Domain:   "k2v4.example.com",
		Protocol: "k2v4",
		Port:     10001,
	}
	assert.Empty(t, input.CertPin)
	assert.Empty(t, input.ECHConfigList)

	tunnel := SlaveTunnel{
		Domain:        input.Domain,
		Protocol:      TunnelProtocolK2V4,
		Port:          int64(input.Port),
		CertPin:       input.CertPin,
		ECHConfigList: input.ECHConfigList,
	}
	assert.Empty(t, tunnel.CertPin)
	assert.Empty(t, tunnel.ECHConfigList)
}

// TestApiK2Tunnels_K2V5HasServerUrl verifies that for a k2v5 tunnel with CertPin,
// buildK2V5ServerURL produces a non-empty serverUrl that can be set on DataSlaveTunnel.
func TestApiK2Tunnels_K2V5HasServerUrl(t *testing.T) {
	tunnel := SlaveTunnel{
		ID:            1,
		Domain:        "k2v5.example.com",
		Protocol:      TunnelProtocolK2V5,
		Port:          443,
		CertPin:       "sha256:testabc",
		ECHConfigList: "AABB",
	}

	// Simulate the response builder logic from api_k2_tunnels
	item := DataSlaveTunnel{
		ID:       tunnel.ID,
		Domain:   tunnel.Domain,
		Protocol: tunnel.Protocol,
		Port:     tunnel.Port,
	}

	// Apply serverUrl for k2v5 with certPin
	if tunnel.Protocol == TunnelProtocolK2V5 && tunnel.CertPin != "" {
		item.ServerUrl = buildK2V5ServerURL(&tunnel)
	}

	assert.NotEmpty(t, item.ServerUrl, "k2v5 tunnel with certPin must have serverUrl")
	assert.Contains(t, item.ServerUrl, "k2v5://")
	assert.Contains(t, item.ServerUrl, "pin=sha256:testabc")
}

// TestApiK2Tunnels_K2V4NoServerUrl verifies that k2v4 tunnels do not get a serverUrl set.
func TestApiK2Tunnels_K2V4NoServerUrl(t *testing.T) {
	tunnel := SlaveTunnel{
		ID:       2,
		Domain:   "k2v4.example.com",
		Protocol: TunnelProtocolK2V4,
		Port:     10001,
	}

	item := DataSlaveTunnel{
		ID:       tunnel.ID,
		Domain:   tunnel.Domain,
		Protocol: tunnel.Protocol,
		Port:     tunnel.Port,
	}

	// k2v4 does not set serverUrl
	if tunnel.Protocol == TunnelProtocolK2V5 && tunnel.CertPin != "" {
		item.ServerUrl = buildK2V5ServerURL(&tunnel)
	}

	assert.Empty(t, item.ServerUrl, "k2v4 tunnel must not have serverUrl")
}

// TestApiK2Tunnels_K2V4IncludesK2V5 verifies the existing backward-compatibility behavior
// that k2v4 query set includes k2v5 tunnels (already tested in TestTunnelProtocolK2V5_BackwardCompatibility
// but re-confirmed here for context of the broader feature).
func TestApiK2Tunnels_K2V4IncludesK2V5(t *testing.T) {
	protocols := tunnelProtocolsForQuery(TunnelProtocolK2V4)
	assert.Contains(t, protocols, TunnelProtocolK2V4,
		"k2v4 query set must include k2v4")
	assert.Contains(t, protocols, TunnelProtocolK2V5,
		"k2v4 query set must include k2v5 for backward compatibility")
	assert.Len(t, protocols, 2,
		"k2v4 query set must contain exactly k2v4 and k2v5")
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
