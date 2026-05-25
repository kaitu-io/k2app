package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// callCore posts a {action, params} body to handleCore and returns the
// decoded Response. Mirrors how the webapp talks to daemon.
func callCore(t *testing.T, d *Daemon, action string, params map[string]any) Response {
	t.Helper()
	body, _ := json.Marshal(Request{Action: action, Params: params})
	req := httptest.NewRequest(http.MethodPost, "/api/core", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	d.handleCore(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return resp
}

// decodeData re-marshals resp.Data into the requested type. Daemon's
// Response.Data is `any`, so callers do a second-pass unmarshal — same
// pattern the webapp uses.
func decodeData[T any](t *testing.T, resp Response) T {
	t.Helper()
	raw, _ := json.Marshal(resp.Data)
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return out
}

func TestAPI_AppBypassGet_Empty(t *testing.T) {
	d := New()
	resp := callCore(t, d, "app-bypass-get", nil)
	if resp.Code != 0 {
		t.Errorf("Response.Code = %d, want 0; message=%s", resp.Code, resp.Message)
	}
	state := decodeData[appBypassState](t, resp)
	if state.FeatureSupported != isAppBypassPlatformSupported() {
		t.Errorf("FeatureSupported mismatch")
	}
	if state.Region != "" {
		t.Errorf("Region should be empty on fresh daemon, got %q", state.Region)
	}
}

func TestAPI_AppBypassGet_Populated(t *testing.T) {
	d := New()
	d.appBypassOverrides = AppBypassOverrides{
		Region:      "cn",
		ProcessAdds: []string{"Steam"},
	}
	resp := callCore(t, d, "app-bypass-get", nil)
	state := decodeData[appBypassState](t, resp)
	if state.Region != "cn" {
		t.Errorf("Region = %q, want cn", state.Region)
	}
	if !containsAppBypassStr(state.Custom.ProcessAdds, "Steam") {
		t.Errorf("ProcessAdds should contain Steam")
	}
}
