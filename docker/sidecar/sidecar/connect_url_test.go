package sidecar

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConnectURL_Full(t *testing.T) {
	url := "k2v5://udid:token@hk1.example.com:443?ech=AABBCCDD&pin=sha256:abc123&insecure=1"
	certPin, echConfig := ParseConnectURL(url)
	assert.Equal(t, "sha256:abc123", certPin)
	assert.Equal(t, "AABBCCDD", echConfig)
}

func TestParseConnectURL_NoECH(t *testing.T) {
	url := "k2v5://udid:token@test.com:443?pin=sha256:xyz"
	certPin, echConfig := ParseConnectURL(url)
	assert.Equal(t, "sha256:xyz", certPin)
	assert.Equal(t, "", echConfig)
}

func TestParseConnectURL_InvalidURL(t *testing.T) {
	certPin, echConfig := ParseConnectURL("not a valid url %%%")
	assert.Equal(t, "", certPin)
	assert.Equal(t, "", echConfig)
}

func TestParseConnectURL_EmptyString(t *testing.T) {
	certPin, echConfig := ParseConnectURL("")
	assert.Equal(t, "", certPin)
	assert.Equal(t, "", echConfig)
}

func TestTunnelConfig_MarshalWithCertPin(t *testing.T) {
	tc := TunnelConfig{
		Domain:        "test.com",
		Protocol:      "k2v5",
		Port:          443,
		CertPin:       "sha256:abc",
		ECHConfigList: "AABB",
	}
	data, err := json.Marshal(tc)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"certPin":"sha256:abc"`)
	assert.Contains(t, string(data), `"echConfigList":"AABB"`)
}
