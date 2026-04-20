package rule

import (
	"bytes"
	"testing"
	"time"
)

// makeTestK2B creates a minimal valid k2b binary via WriteBundle and returns
// the raw bytes.
func makeTestK2B(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := WriteBundle(&buf, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	return buf.Bytes()
}

// TestBackoffDuration verifies the exponential backoff sequence and cap.
func TestBackoffDuration(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{10, 60 * time.Second}, // capped
		{20, 60 * time.Second}, // still capped
	}
	for _, c := range cases {
		got := backoffDuration(c.attempt)
		if got != c.want {
			t.Errorf("backoffDuration(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}
