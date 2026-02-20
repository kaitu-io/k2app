package main

import (
	"bytes"
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
