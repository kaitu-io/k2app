package main

import (
	"testing"
)

func TestCmdConnectNoArgs(t *testing.T) {
	code := cmdConnect(nil)
	if code != 1 {
		t.Errorf("expected exit 1 for no args, got %d", code)
	}
}

func TestCmdConnectBadURL(t *testing.T) {
	code := cmdConnect([]string{"not-a-url"})
	if code != 1 {
		t.Errorf("expected exit 1 for bad URL, got %d", code)
	}
}
