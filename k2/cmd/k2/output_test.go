package main

import (
	"bytes"
	"testing"
)

func TestOutputHuman(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf}
	o.printResult("Connected")
	if got := buf.String(); got != "Connected\n" {
		t.Errorf("got %q", got)
	}
}

func TestOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf, json: true}
	o.ok(map[string]any{"state": "connected"})
	want := `{"data":{"state":"connected"},"ok":true}` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputJSONError(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf, json: true}
	o.fail("DAEMON_NOT_RUNNING", "daemon not running", "run: k2 setup")
	want := `{"code":"DAEMON_NOT_RUNNING","error":"daemon not running","hint":"run: k2 setup","ok":false}` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
