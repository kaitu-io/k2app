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
