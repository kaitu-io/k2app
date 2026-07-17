//go:build linux

package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleSetCredentialPersistsConfig locks in Cleanup #1: applyCredential
// must write BOTH the source config (k2r.yml, read by `k2r run` on full
// restart) and the reconnect cache (state.json). A panel-set credential that
// only landed in state.json would be lost on the next full restart.
func TestHandleSetCredentialPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	g := &Gateway{}
	url := "k2subs://router-x:tok123@center.example/api/subs"
	cfg, err := g.applyCredential(url)
	if err != nil {
		t.Fatalf("applyCredential: %v", err)
	}
	if cfg == nil {
		t.Fatal("nil cfg")
	}
	st := loadState()
	if st == nil || st.Config == nil {
		t.Fatal("state.json not persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, "k2r.yml")); err != nil {
		t.Fatalf("k2r.yml not persisted: %v", err)
	}
}

func TestHandleSetCredentialRejectsBadURL(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	g := &Gateway{}
	if _, err := g.applyCredential("not-a-url"); err == nil {
		t.Fatal("want error on bad url")
	}
}

func TestPingDiscoverySignature(t *testing.T) {
	withTempStateDir(t)
	g := &Gateway{version: "0.4.7-test"}

	// 未配置
	w := httptest.NewRecorder()
	g.handlePing(w, httptest.NewRequest("GET", "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if resp["k2r"] != true {
		t.Fatal("missing k2r:true signature")
	}
	if resp["configured"] != false {
		t.Fatalf("configured = %v, want false", resp["configured"])
	}
	if resp["version"] != "0.4.7-test" {
		t.Fatalf("version = %v", resp["version"])
	}

	// 配置后 configured:true
	if err := PersistConfig(GatewayFromURL("k2v5://u:t@h.example.com:443/")); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	g.handlePing(w, httptest.NewRequest("GET", "/ping", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["configured"] != true {
		t.Fatal("configured must flip to true after PersistConfig")
	}
}

func TestRootNoticePage(t *testing.T) {
	w := httptest.NewRecorder()
	handleRootNotice(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	// 品牌中立:不得出现品牌词(brand split 宪法)
	body := w.Body.String()
	for _, banned := range []string{"Kaitu", "kaitu", "开途", "Overleap", "overleap"} {
		if strings.Contains(body, banned) {
			t.Fatalf("notice page must be brand-neutral, found %q", banned)
		}
	}
	// 非 / 路径 404
	w = httptest.NewRecorder()
	handleRootNotice(w, httptest.NewRequest("GET", "/nonexistent", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-root path: code = %d, want 404", w.Code)
	}
}
