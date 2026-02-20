package sidecar

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildServerURL_Full(t *testing.T) {
	raw := "k2v5://udid:token@hk1.example.com:443?ech=AABBCCDD&pin=sha256:abc123&insecure=1"
	url := BuildServerURL(raw, "hk1.example.com", 443, 10020, 10119)
	assert.Equal(t, "k2v5://hk1.example.com:443?ech=AABBCCDD&pin=sha256:abc123&hop=10020-10119", url)
}

func TestBuildServerURL_NoHop(t *testing.T) {
	raw := "k2v5://udid:token@hk1.example.com:443?ech=AABBCCDD&pin=sha256:abc123"
	url := BuildServerURL(raw, "hk1.example.com", 443, 0, 0)
	assert.Equal(t, "k2v5://hk1.example.com:443?ech=AABBCCDD&pin=sha256:abc123", url)
}

func TestBuildServerURL_NoECH(t *testing.T) {
	raw := "k2v5://udid:token@test.com:443?pin=sha256:xyz"
	url := BuildServerURL(raw, "node.example.com", 443, 0, 0)
	assert.Equal(t, "k2v5://node.example.com:443?pin=sha256:xyz", url)
	assert.NotContains(t, url, "ech=")
}

func TestBuildServerURL_OverridesDomainPort(t *testing.T) {
	raw := "k2v5://udid:token@original.com:8443?ech=AA&pin=sha256:bb"
	url := BuildServerURL(raw, "configured.com", 443, 0, 0)
	assert.Contains(t, url, "k2v5://configured.com:443")
	assert.NotContains(t, url, "original.com")
}

func TestBuildServerURL_InvalidURL(t *testing.T) {
	url := BuildServerURL("not a valid url %%%", "test.com", 443, 0, 0)
	assert.Equal(t, "", url)
}

func TestBuildServerURL_EmptyString(t *testing.T) {
	url := BuildServerURL("", "test.com", 443, 0, 0)
	assert.Equal(t, "", url)
}

func TestBuildServerURL_NoParams(t *testing.T) {
	raw := "k2v5://udid:token@test.com:443"
	url := BuildServerURL(raw, "test.com", 443, 0, 0)
	assert.Equal(t, "", url, "should return empty when no ech or pin params")
}

func TestTunnelConfig_MarshalWithServerURL(t *testing.T) {
	serverURL := "k2v5://test.com:443?ech=AABB&pin=sha256:abc"
	tc := TunnelConfig{
		Domain:    "test.com",
		Protocol:  "k2v5",
		Port:      443,
		ServerURL: serverURL,
	}
	data, err := json.Marshal(tc)
	require.NoError(t, err)

	// Unmarshal back and verify the field round-trips correctly
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, serverURL, parsed["serverUrl"])
}
