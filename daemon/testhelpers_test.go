package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

// writeCNBundle drops a minimal cn.krs file in dir with WeChat patterns for
// Darwin + Windows, plus a named set so get-presets can count sets.
// Used by classify-apps and get-presets tests.
func writeCNBundle(t *testing.T, dir string) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{
		Sets: []krs.NamedSet{{Name: "cn-sites"}},
		Apps: &krs.AppPatterns{
			Darwin:  krs.DarwinPatterns{Apps: []string{"WeChat*"}},
			Windows: krs.WindowsPatterns{Apps: []string{"WeChat*"}},
		},
	}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}
}

// writeCNBundleAndroid drops a minimal cn.krs file in dir with Android
// patterns (app package + installer). Used by classify-apps Android tests.
func writeCNBundleAndroid(t *testing.T, dir string) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{
		Sets: []krs.NamedSet{{Name: "cn-sites"}},
		Apps: &krs.AppPatterns{
			Android: krs.AndroidPatterns{
				Apps:       []string{"com.tencent.mm"},
				Installers: []string{"com.tencent.android.qqdownloader"},
			},
		},
	}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cn.krs"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write preset: %v", err)
	}
}

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
