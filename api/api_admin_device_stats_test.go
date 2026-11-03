package center

import "testing"

// Clients send "macos" (webapp tauri-k2.ts); the summary once keyed macOS as
// "darwin", which no client ever sends, so every Mac landed in unknownDevices.
func TestPlatformSummaryBucket(t *testing.T) {
	cases := map[string]string{
		"macos":   "desktop",
		"windows": "desktop",
		"linux":   "desktop",
		"ios":     "mobile",
		"android": "mobile",
		"unknown": "unknown",
		"web":     "unknown",
		"darwin":  "unknown",
		"":        "unknown",
	}
	for in, want := range cases {
		if got := platformSummaryBucket(in); got != want {
			t.Errorf("platformSummaryBucket(%q) = %q, want %q", in, got, want)
		}
	}
}
