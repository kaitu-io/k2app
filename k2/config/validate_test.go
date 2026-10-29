package config

import (
	"errors"
	"testing"
)

func TestValidateClient_Valid(t *testing.T) {
	cfg := &ClientConfig{
		Server: "k2v5://udid:token@1.2.3.4:443",
		Listen: "127.0.0.1:1777",
		Mode:   "tun",
		Log:    LogConfig{Level: "info"},
	}
	if err := ValidateClient(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateClient_BadServerURL(t *testing.T) {
	cfg := &ClientConfig{
		Server: "not a valid url ://bad",
		Listen: "127.0.0.1:1777",
		Mode:   "tun",
		Log:    LogConfig{Level: "info"},
	}
	err := ValidateClient(cfg)
	if err == nil {
		t.Fatal("expected error for bad server URL, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "server" {
		t.Errorf("Field = %q, want server", ve.Field)
	}
}

func TestValidateClient_BadListenAddr(t *testing.T) {
	cfg := &ClientConfig{
		Server: "k2v5://udid:token@1.2.3.4:443",
		Listen: "not_valid_addr",
		Mode:   "tun",
		Log:    LogConfig{Level: "info"},
	}
	err := ValidateClient(cfg)
	if err == nil {
		t.Fatal("expected error for bad listen addr, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "listen" {
		t.Errorf("Field = %q, want listen", ve.Field)
	}
}

func TestValidateClient_BadMode(t *testing.T) {
	cfg := &ClientConfig{
		Server: "k2v5://udid:token@1.2.3.4:443",
		Listen: "127.0.0.1:1777",
		Mode:   "invalid_mode",
		Log:    LogConfig{Level: "info"},
	}
	err := ValidateClient(cfg)
	if err == nil {
		t.Fatal("expected error for bad mode, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "mode" {
		t.Errorf("Field = %q, want mode", ve.Field)
	}
}

func TestValidateClient_BadLogLevel(t *testing.T) {
	cfg := &ClientConfig{
		Server: "k2v5://udid:token@1.2.3.4:443",
		Listen: "127.0.0.1:1777",
		Mode:   "tun",
		Log:    LogConfig{Level: "verbose"},
	}
	err := ValidateClient(cfg)
	if err == nil {
		t.Fatal("expected error for bad log level, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "log.level" {
		t.Errorf("Field = %q, want log.level", ve.Field)
	}
}

func TestValidateServer_Valid(t *testing.T) {
	cfg := &ServerConfig{
		Listen: ":443",
		Log:    LogConfig{Level: "info"},
	}
	if err := ValidateServer(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateServer_EmptyListen(t *testing.T) {
	// Empty listen is OK — setServerDefaults will apply ":443"
	cfg := &ServerConfig{
		Listen: "",
		Log:    LogConfig{Level: "info"},
	}
	if err := ValidateServer(cfg); err != nil {
		t.Errorf("empty listen should be OK (default applied), got error: %v", err)
	}
}

// TestValidateClient_ReservedViaScheme verifies that routes using the
// reserved device:// / auto:// schemes are rejected with a clear error
// pointing at the specific route index. This is a forward-compat gate
// for the k2p home feature (design reserved, implementation deferred).
func TestValidateClient_ReservedViaScheme(t *testing.T) {
	cases := []struct {
		name    string
		via     string
		wantErr bool
	}{
		{"device scheme rejected", "device://abc123", true},
		{"auto scheme rejected", "auto://best", true},
		{"auto with country rejected", "auto://best/us", true},
		{"direct sentinel allowed", "direct", false},
		{"reject sentinel allowed", "reject", false},
		{"empty allowed", "", false},
		{"k2v5 allowed", "k2v5://udid:token@1.2.3.4:443", false},
		{"k2p allowed", "k2p://peer@host:443", false},
		{"k2subs allowed", "k2subs://sub@host:443", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &ClientConfig{
				Routes: []RouteConfig{{Via: tc.via}},
			}
			err := ValidateClient(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("via=%q: expected error, got nil", tc.via)
				}
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("via=%q: expected *ValidationError, got %T: %v", tc.via, err, err)
				}
				if ve.Field != "routes[0].via" {
					t.Errorf("via=%q: Field = %q, want routes[0].via", tc.via, ve.Field)
				}
			} else {
				if err != nil {
					t.Errorf("via=%q: unexpected error: %v", tc.via, err)
				}
			}
		})
	}
}

func TestViaScheme(t *testing.T) {
	cases := map[string]string{
		"k2v5://host:443":   "k2v5",
		"k2p://peer@host":   "k2p",
		"k2subs://s@host":   "k2subs",
		"device://abc":      "device",
		"auto://best":       "auto",
		"auto://best/us":    "auto",
		"direct":            "",
		"reject":            "",
		"":                  "",
		"://empty-scheme":   "",
	}
	for in, want := range cases {
		if got := ViaScheme(in); got != want {
			t.Errorf("ViaScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsReservedVia(t *testing.T) {
	cases := map[string]bool{
		"device://abc":              true,
		"auto://best":               true,
		"auto://best/us":            true,
		"k2v5://udid:tok@h:443":     false,
		"k2p://peer@h:443":          false,
		"k2subs://sub@h:443":        false,
		"direct":                    false,
		"reject":                    false,
		"":                          false,
	}
	for via, want := range cases {
		if got := IsReservedVia(via); got != want {
			t.Errorf("IsReservedVia(%q) = %v, want %v", via, got, want)
		}
	}
}

func TestIsOutboundURL_RespectsReservedSchemes(t *testing.T) {
	// Guard: reserved schemes must NOT be treated as wire outbounds even though
	// they have URL shape. This prevents buildRouteEntries from assigning them
	// a Target index.
	cases := map[string]bool{
		"k2v5://udid:tok@h:443": true,
		"k2p://peer@h:443":      true,
		"k2subs://sub@h:443":    true,
		"device://abc":          false,
		"auto://best":           false,
		"direct":                false,
		"":                      false,
		"http://example.com":    false,
	}
	for via, want := range cases {
		if got := IsOutboundURL(via); got != want {
			t.Errorf("IsOutboundURL(%q) = %v, want %v", via, got, want)
		}
	}
}
