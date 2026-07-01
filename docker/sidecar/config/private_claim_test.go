package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// TestK2CenterConfig_PrivateClaimFromYAML verifies the private-node claim token
// written by entrypoint.sh under k2_center.private_claim loads into the config.
// This mirrors how K2_NODE_SECRET flows (env -> YAML -> config field).
func TestK2CenterConfig_PrivateClaimFromYAML(t *testing.T) {
	t.Run("claim present in YAML", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yml := `
k2_center:
  enabled: true
  base_url: "https://k2.52j.me"
  secret: "node-secret"
  private_claim: "tok123"
`
		require.NoError(t, os.WriteFile(path, []byte(yml), 0644))

		cfg := &Config{}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal(data, cfg))

		assert.Equal(t, "tok123", cfg.K2Center.PrivateClaim)
	})

	t.Run("claim absent yields empty (shared node)", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		yml := `
k2_center:
  enabled: true
  base_url: "https://k2.52j.me"
  secret: "node-secret"
`
		require.NoError(t, os.WriteFile(path, []byte(yml), 0644))

		cfg := &Config{}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, yaml.Unmarshal(data, cfg))

		assert.Equal(t, "", cfg.K2Center.PrivateClaim)
	})
}
