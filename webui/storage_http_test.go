//go:build linux

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"
)

// newTestStorageHandler creates an http.HandlerFunc over a fresh temp-dir
// Storage with a deterministic test key.
func newTestStorageHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	dir := t.TempDir()
	s := NewStorageWithKey(filepath.Join(dir, "storage.json"), "test")
	return NewStorageHandler(s)
}

// storageReq builds a JSON body, calls the given handler, and returns the
// recorded response.
func storageReq(t *testing.T, h http.HandlerFunc, action, key string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"action": action,
		"key":    key,
		"value":  value,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("storageReq marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/storage", bytes.NewReader(data))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// decodeResponse unmarshals the recorder body into the shared response envelope.
func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) response {
	t.Helper()
	var resp response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	return resp
}

// TestStorageAPI_SetAndGet verifies that a string value can be stored and
// retrieved correctly.
func TestStorageAPI_SetAndGet(t *testing.T) {
	h := newTestStorageHandler(t)

	w := storageReq(t, h, "set", "mykey", "myvalue")
	if w.Code != http.StatusOK {
		t.Fatalf("set: expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("set: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, h, "get", "mykey", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("get: expected code 0, got %d: %s", resp.Code, resp.Message)
	}
	got, ok := resp.Data.(string)
	if !ok {
		t.Fatalf("get: expected string data, got %T: %v", resp.Data, resp.Data)
	}
	if got != "myvalue" {
		t.Fatalf("get: expected %q, got %q", "myvalue", got)
	}
}

// TestStorageAPI_GetMissing verifies that getting a nonexistent key returns
// code=0 with null data.
func TestStorageAPI_GetMissing(t *testing.T) {
	h := newTestStorageHandler(t)

	w := storageReq(t, h, "get", "no-such-key", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}
	if resp.Data != nil {
		t.Fatalf("expected nil data for missing key, got %v", resp.Data)
	}
}

// TestStorageAPI_NestedObject verifies that a JSON object value round-trips
// with its field types preserved.
func TestStorageAPI_NestedObject(t *testing.T) {
	h := newTestStorageHandler(t)

	obj := map[string]any{"a": float64(1), "b": "two"}
	w := storageReq(t, h, "set", "obj", obj)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("set: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, h, "get", "obj", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("get: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	got, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T: %v", resp.Data, resp.Data)
	}
	if got["a"] != float64(1) {
		t.Errorf("expected a=1, got %v", got["a"])
	}
	if got["b"] != "two" {
		t.Errorf("expected b=%q, got %v", "two", got["b"])
	}
}

// TestStorageAPI_Has verifies has-before-set returns false and has-after-set
// returns true.
func TestStorageAPI_Has(t *testing.T) {
	h := newTestStorageHandler(t)

	w := storageReq(t, h, "has", "k", nil)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("has (missing): expected code 0, got %d", resp.Code)
	}
	if resp.Data != false {
		t.Fatalf("has (missing): expected false, got %v", resp.Data)
	}

	storageReq(t, h, "set", "k", "v")

	w = storageReq(t, h, "has", "k", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("has (existing): expected code 0, got %d", resp.Code)
	}
	if resp.Data != true {
		t.Fatalf("has (existing): expected true, got %v", resp.Data)
	}
}

// TestStorageAPI_Keys verifies that keys returns all stored keys.
func TestStorageAPI_Keys(t *testing.T) {
	h := newTestStorageHandler(t)

	storageReq(t, h, "set", "alpha", "1")
	storageReq(t, h, "set", "beta", "2")

	w := storageReq(t, h, "keys", "", nil)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("keys: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	raw, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("keys: expected []any, got %T: %v", resp.Data, resp.Data)
	}
	if len(raw) != 2 {
		t.Fatalf("keys: expected 2 keys, got %d: %v", len(raw), raw)
	}

	keys := make([]string, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("keys: element %d is %T, not string", i, v)
		}
		keys[i] = s
	}
	sort.Strings(keys)

	if keys[0] != "alpha" || keys[1] != "beta" {
		t.Errorf("keys: expected [alpha beta], got %v", keys)
	}
}

// TestStorageAPI_Remove verifies that a key is absent after removal.
func TestStorageAPI_Remove(t *testing.T) {
	h := newTestStorageHandler(t)

	storageReq(t, h, "set", "temp", "data")

	w := storageReq(t, h, "has", "temp", nil)
	resp := decodeResponse(t, w)
	if resp.Data != true {
		t.Fatalf("has before remove: expected true, got %v", resp.Data)
	}

	w = storageReq(t, h, "remove", "temp", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("remove: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, h, "has", "temp", nil)
	resp = decodeResponse(t, w)
	if resp.Data != false {
		t.Fatalf("has after remove: expected false, got %v", resp.Data)
	}
}

// TestStorageAPI_Clear verifies that all keys are gone after a clear.
func TestStorageAPI_Clear(t *testing.T) {
	h := newTestStorageHandler(t)

	storageReq(t, h, "set", "x", "1")
	storageReq(t, h, "set", "y", "2")

	w := storageReq(t, h, "clear", "", nil)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("clear: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, h, "keys", "", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("keys after clear: expected code 0, got %d", resp.Code)
	}

	raw, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("keys after clear: expected []any, got %T: %v", resp.Data, resp.Data)
	}
	if len(raw) != 0 {
		t.Fatalf("keys after clear: expected 0 keys, got %d: %v", len(raw), raw)
	}
}

// TestStorageAPI_MethodNotAllowed verifies that a GET request returns 405.
func TestStorageAPI_MethodNotAllowed(t *testing.T) {
	h := newTestStorageHandler(t)

	r := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestPlatformHandler verifies that NewPlatformHandler returns the expected
// fields for a gateway-style PlatformInfo.
func TestPlatformHandler(t *testing.T) {
	h := NewPlatformHandler(PlatformInfo{
		PlatformType: "gateway",
		Version:      "test-version",
		Commit:       "abc123",
		Arch:         "amd64",
	})

	r := httptest.NewRequest(http.MethodGet, "/api/platform", nil)
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T: %v", resp.Data, resp.Data)
	}

	checks := map[string]string{
		"platformType": "gateway",
		"version":      "test-version",
		"commit":       "abc123",
		"arch":         "amd64",
		"os":           "linux",
	}
	for field, want := range checks {
		got, ok := data[field]
		if !ok {
			t.Errorf("platform: missing field %q", field)
			continue
		}
		if got != want {
			t.Errorf("platform: %s = %q, want %q", field, got, want)
		}
	}
}
