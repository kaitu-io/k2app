package appext

import (
	"strings"
	"testing"
)

func TestRelayFetch_Export(t *testing.T) {
	// Smoke: bad input returns a non-zero envelope (never panics / never empty).
	out := RelayFetch("not json")
	if out == "" || !strings.Contains(out, `"code"`) {
		t.Fatalf("export must return envelope, got %q", out)
	}
}
