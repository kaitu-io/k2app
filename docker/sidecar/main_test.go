package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestK2V5ConfigTemplate_ContainsCertDir(t *testing.T) {
	data := K2V5ConfigData{
		CertDir:     "/etc/k2v5",
		CertPath:    "/etc/kaitu/certs/server-cert.pem",
		KeyPath:     "/etc/kaitu/certs/server-key.pem",
		K2Domain:    "test.example.com",
		K2V4Port:    "8443",
		K2OCDomain:  "",
		K2OCPort:    "10001",
		CenterURL:   "https://k2.52j.me",
		LogLevel:    "info",
		HasOCDomain: false,
	}

	tmpl, err := template.New("k2v5-config").Parse(k2v5ConfigTemplate)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	require.NoError(t, err)

	output := buf.String()

	// Must contain cert_dir pointing to /etc/k2v5
	assert.Contains(t, output, `cert_dir: "/etc/k2v5"`,
		"template must include cert_dir field")

	// tls entries should have a comment indicating they are dormant
	if strings.Contains(output, "tls:") {
		assert.Contains(t, output, "dormant",
			"tls entries should be annotated as dormant")
	}
}

func TestReadConnectURL_FindsFileAndBuildsURL(t *testing.T) {
	dir := t.TempDir()

	// Write a valid k2v5:// connect URL
	content := "k2v5://udid:token@1.2.3.4:443?ech=AABBCC&pin=sha256:testpin123&insecure=1\n"
	err := os.WriteFile(filepath.Join(dir, "connect-url.txt"), []byte(content), 0644)
	require.NoError(t, err)

	result := readConnectURL(dir, "test.example.com", 443, 10020, 10119)

	assert.NotEmpty(t, result, "should return a non-empty server URL")
	assert.Contains(t, result, "k2v5://test.example.com:443")
	assert.Contains(t, result, "ech=AABBCC")
	assert.Contains(t, result, "pin=sha256:testpin123")
	assert.Contains(t, result, "hop=10020-10119")
	// Auth credentials must be stripped
	assert.NotContains(t, result, "udid")
	assert.NotContains(t, result, "token")
	// Dev flags must be stripped
	assert.NotContains(t, result, "insecure")
}

func TestReadConnectURL_ReturnsEmptyWhenMissing(t *testing.T) {
	dir := t.TempDir()

	result := readConnectURL(dir, "test.example.com", 443, 10020, 10119)

	assert.Empty(t, result, "should return empty string when file is missing")
}
