package center

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
