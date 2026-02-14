package main

import (
	"testing"

	"github.com/kaitu-io/k2/daemon"
)

func TestOpenBrowserURL(t *testing.T) {
	// Verify the URL is constructed correctly from daemon.DefaultAddr.
	expected := "http://127.0.0.1:1777"
	got := "http://" + daemon.DefaultAddr
	if got != expected {
		t.Fatalf("expected URL %s, got %s", expected, got)
	}
}
