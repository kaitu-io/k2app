package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSetServerDefaults_UsageReportEnv verifies the three usage-reporter env
// vars (K2_USAGE_REPORT_URL / K2_NODE_SECRET / K2_NODE_IPV4) populate the
// matching ServerConfig fields, and that with the env unset the fields stay
// empty (the enable gate that keeps shared-pool nodes silent).
func TestSetServerDefaults_UsageReportEnv(t *testing.T) {
	t.Run("env set → fields populated", func(t *testing.T) {
		t.Setenv("K2_USAGE_REPORT_URL", "https://center.example.com")
		t.Setenv("K2_NODE_SECRET", "topsecret")
		t.Setenv("K2_NODE_IPV4", "203.0.113.9")

		cfg := DefaultServerConfig()

		if cfg.UsageReportURL != "https://center.example.com" {
			t.Errorf("UsageReportURL = %q, want https://center.example.com", cfg.UsageReportURL)
		}
		if cfg.NodeSecret != "topsecret" {
			t.Errorf("NodeSecret = %q, want topsecret", cfg.NodeSecret)
		}
		if cfg.NodeIPv4 != "203.0.113.9" {
			t.Errorf("NodeIPv4 = %q, want 203.0.113.9", cfg.NodeIPv4)
		}
	})

	t.Run("env unset → fields empty (gate stays closed)", func(t *testing.T) {
		// These three vars are not part of the test runner's environment, so a
		// fresh defaults pass must leave the fields empty — the enable gate that
		// keeps shared-pool nodes from ever starting the reporter.
		cfg := &ServerConfig{}
		setServerDefaults(cfg)
		if cfg.UsageReportURL != "" {
			t.Errorf("UsageReportURL = %q, want empty", cfg.UsageReportURL)
		}
		if cfg.NodeSecret != "" {
			t.Errorf("NodeSecret = %q, want empty", cfg.NodeSecret)
		}
		if cfg.NodeIPv4 != "" {
			t.Errorf("NodeIPv4 = %q, want empty", cfg.NodeIPv4)
		}
	})
}

// TestNodeCutoffReportEnv verifies K2_NODE_CUTOFF_REPORT gates the LEGACY
// node-level usage reporter separately from the UsageReportURL/NodeSecret
// creds gate: "1" enables it, unset/empty leaves it false (dormant) so the
// legacy reporter's broken verdict handling can't halt a node by accident.
func TestNodeCutoffReportEnv(t *testing.T) {
	t.Run("K2_NODE_CUTOFF_REPORT=1 → NodeCutoffReport true", func(t *testing.T) {
		t.Setenv("K2_NODE_CUTOFF_REPORT", "1")
		cfg := &ServerConfig{}
		setServerDefaults(cfg)
		if !cfg.NodeCutoffReport {
			t.Fatal("K2_NODE_CUTOFF_REPORT=1 must set NodeCutoffReport")
		}
	})

	t.Run("env unset → default false", func(t *testing.T) {
		cfg := &ServerConfig{}
		setServerDefaults(cfg)
		if cfg.NodeCutoffReport {
			t.Fatal("default must be false")
		}
	})
}

// TestServerConfig_SecretYAMLHygiene proves the secret + identity fields carry
// yaml:"-" so yaml.Marshal never serializes them into any config file, while a
// normal yaml field (UsageReportURL) does appear.
func TestServerConfig_SecretYAMLHygiene(t *testing.T) {
	cfg := &ServerConfig{
		UsageReportURL: "https://center.example.com",
		NodeSecret:     "shh",
		NodeIPv4:       "1.2.3.4",
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	got := string(out)

	if strings.Contains(got, "shh") {
		t.Errorf("marshalled yaml leaked NodeSecret:\n%s", got)
	}
	if strings.Contains(got, "1.2.3.4") {
		t.Errorf("marshalled yaml leaked NodeIPv4:\n%s", got)
	}
	if !strings.Contains(got, "https://center.example.com") {
		t.Errorf("marshalled yaml missing UsageReportURL (a normal yaml field):\n%s", got)
	}
}
