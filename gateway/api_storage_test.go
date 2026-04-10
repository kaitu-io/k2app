//go:build linux

package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"
)

// newTestGateway creates a Gateway with a temp storage directory.
func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	dir := t.TempDir()
	g := New("test-version", "abc123", "amd64")
	g.storage = newStorage(filepath.Join(dir, "storage.json"), deriveKey("test"))
	return g
}

// storageReq builds a JSON body, creates a POST request, calls g.handleStorage,
// and returns the recorded response.
func storageReq(t *testing.T, g *Gateway, action, key string, value any) *httptest.ResponseRecorder {
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
	g.handleStorage(w, r)
	return w
}

// decodeResponse unmarshals the recorder body into a Response.
func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decodeResponse: %v", err)
	}
	return resp
}

// TestStorageAPI_SetAndGet verifies that a string value can be stored and
// retrieved correctly.
func TestStorageAPI_SetAndGet(t *testing.T) {
	g := newTestGateway(t)

	w := storageReq(t, g, "set", "mykey", "myvalue")
	if w.Code != http.StatusOK {
		t.Fatalf("set: expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("set: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, g, "get", "mykey", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("get: expected code 0, got %d: %s", resp.Code, resp.Message)
	}
	// The handler JSON-decodes the stored value back — a string value becomes a
	// JSON string which decodes again to a Go string.
	got, ok := resp.Data.(string)
	if !ok {
		t.Fatalf("get: expected string data, got %T: %v", resp.Data, resp.Data)
	}
	if got != "myvalue" {
		t.Fatalf("get: expected %q, got %q", "myvalue", got)
	}
}

// TestStorageAPI_GetMissing verifies that getting a nonexistent key returns
// code=0 with null data (JSON null → nil).
func TestStorageAPI_GetMissing(t *testing.T) {
	g := newTestGateway(t)

	w := storageReq(t, g, "get", "no-such-key", nil)
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

// TestStorageAPI_NestedObject verifies that a JSON object value is stored and
// retrieved with its field types preserved.
func TestStorageAPI_NestedObject(t *testing.T) {
	g := newTestGateway(t)

	obj := map[string]any{"a": float64(1), "b": "two"}
	w := storageReq(t, g, "set", "obj", obj)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("set: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, g, "get", "obj", nil)
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

// TestStorageAPI_Has verifies that has returns false for missing keys and true
// for existing keys.
func TestStorageAPI_Has(t *testing.T) {
	g := newTestGateway(t)

	w := storageReq(t, g, "has", "k", nil)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("has (missing): expected code 0, got %d", resp.Code)
	}
	if resp.Data != false {
		t.Fatalf("has (missing): expected false, got %v", resp.Data)
	}

	storageReq(t, g, "set", "k", "v")

	w = storageReq(t, g, "has", "k", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("has (existing): expected code 0, got %d", resp.Code)
	}
	if resp.Data != true {
		t.Fatalf("has (existing): expected true, got %v", resp.Data)
	}
}

// TestStorageAPI_Keys verifies that after setting two keys, the keys action
// returns both of them (in any order).
func TestStorageAPI_Keys(t *testing.T) {
	g := newTestGateway(t)

	storageReq(t, g, "set", "alpha", "1")
	storageReq(t, g, "set", "beta", "2")

	w := storageReq(t, g, "keys", "", nil)
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
	g := newTestGateway(t)

	storageReq(t, g, "set", "temp", "data")

	w := storageReq(t, g, "has", "temp", nil)
	resp := decodeResponse(t, w)
	if resp.Data != true {
		t.Fatalf("has before remove: expected true, got %v", resp.Data)
	}

	w = storageReq(t, g, "remove", "temp", nil)
	resp = decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("remove: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, g, "has", "temp", nil)
	resp = decodeResponse(t, w)
	if resp.Data != false {
		t.Fatalf("has after remove: expected false, got %v", resp.Data)
	}
}

// TestStorageAPI_Clear verifies that all keys are gone after a clear.
func TestStorageAPI_Clear(t *testing.T) {
	g := newTestGateway(t)

	storageReq(t, g, "set", "x", "1")
	storageReq(t, g, "set", "y", "2")

	w := storageReq(t, g, "clear", "", nil)
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("clear: expected code 0, got %d: %s", resp.Code, resp.Message)
	}

	w = storageReq(t, g, "keys", "", nil)
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

// TestStorageAPI_MethodNotAllowed verifies that a GET request to /api/storage
// returns 405 Method Not Allowed.
func TestStorageAPI_MethodNotAllowed(t *testing.T) {
	g := newTestGateway(t)

	r := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	w := httptest.NewRecorder()
	g.handleStorage(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestPlatformAPI verifies that /api/platform returns the expected fields with
// the correct values.
func TestPlatformAPI(t *testing.T) {
	g := newTestGateway(t)

	r := httptest.NewRequest(http.MethodGet, "/api/platform", nil)
	w := httptest.NewRecorder()
	g.handlePlatform(w, r)

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
