package wire

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCaptureResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	got, err := captureResponse(resp)
	if err != nil {
		t.Fatalf("captureResponse: %v", err)
	}
	if got.Status != 200 {
		t.Fatalf("status=%d, want 200", got.Status)
	}
	if got.Body != `{"ok":true}` {
		t.Fatalf("body=%q", got.Body)
	}
	if got.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("headers=%v", got.Headers)
	}
}
