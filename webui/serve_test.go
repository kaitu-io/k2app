//go:build linux

package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHasFileExtension(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/", false},
		{"/dashboard", false},
		{"/account/settings", false},
		{"/assets/app.js", true},
		{"/assets/style.css", true},
		{"/favicon.png", true},
		{"/assets/chunk-abc123.js", true},
	}
	for _, tc := range cases {
		got := hasFileExtension(tc.path)
		if got != tc.want {
			t.Errorf("hasFileExtension(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestNewWebappHandlerNilFS(t *testing.T) {
	orig := webappFS
	webappFS = nil
	defer func() { webappFS = orig }()

	h := NewWebappHandler("1.0.0", "abc123", "amd64")
	if h != nil {
		t.Errorf("expected nil handler when webappFS is nil, got %T", h)
	}
}

func TestWebappHandlerInjectsGatewayGlobal(t *testing.T) {
	if webappFS == nil {
		t.Skip("webappFS is nil (nowebapp build)")
	}

	h := NewWebappHandler("1.2.3", "deadbeef", "arm64")
	if h == nil {
		t.Fatal("NewWebappHandler returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	bodyStr := string(body)

	checks := []string{
		"__K2_GATEWAY__",
		"1.2.3",
	}
	for _, needle := range checks {
		if !strings.Contains(bodyStr, needle) {
			t.Errorf("response body missing %q", needle)
		}
	}
}

func TestWebappHandlerSPAFallback(t *testing.T) {
	if webappFS == nil {
		t.Skip("webappFS is nil (nowebapp build)")
	}

	h := NewWebappHandler("0.0.1", "cafebabe", "amd64")
	if h == nil {
		t.Fatal("NewWebappHandler returned nil")
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("unexpected Content-Type for SPA fallback: %q", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !strings.Contains(string(body), "<html") {
		t.Errorf("SPA fallback did not return HTML content")
	}
}
