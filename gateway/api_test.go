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
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
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

// TestGatewayStatusMarshalJSON_IncludesExtendedFields locks in a real bug:
// gatewayStatus embeds engine.Status, which implements json.Marshaler (to
// compute uptimeSeconds dynamically). Without gatewayStatus's own
// MarshalJSON override, Go silently promotes the embedded method and
// json.Marshal drops every sibling field below with no error — lanSubnets,
// interceptor, and (critically) slots never appeared in any /api/core
// status response, in any mode, until this override was added.
func TestGatewayStatusMarshalJSON_IncludesExtendedFields(t *testing.T) {
	gs := gatewayStatus{
		Status: engine.Status{
			State:       engine.StateConnected,
			ConnectedAt: time.Now().Add(-5 * time.Second),
		},
		LANSubnets:       []string{"10.99.0.0/24"},
		Interceptor:      "nftables-netlink",
		ListenPort:       12345,
		MaxLanClient:     10,
		RouterDeviceMode: "open",
		Slots: []SlotStatus{
			{Slot: 1, State: "running"},
		},
	}
	data, err := json.Marshal(gs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["state"] != "connected" {
		t.Fatalf("state = %v, want connected (from embedded engine.Status)", m["state"])
	}
	if m["lanSubnets"] == nil {
		t.Fatal("lanSubnets missing — embedded engine.Status.MarshalJSON silently ate sibling fields")
	}
	if m["interceptor"] != "nftables-netlink" {
		t.Fatalf("interceptor = %v, want nftables-netlink", m["interceptor"])
	}
	slots, ok := m["slots"].([]any)
	if !ok || len(slots) != 1 {
		t.Fatalf("slots = %v, want a 1-element array", m["slots"])
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

func TestResetWipesBinding(t *testing.T) {
	withTempStateDir(t)
	g := &Gateway{}

	// 造出三件持久化物
	if err := PersistConfig(GatewayFromURL("k2v5://u:t@h.example.com:443/")); err != nil {
		t.Fatal(err)
	}
	saveState(&persistedState{State: "connected"})
	if err := g.setControlKeyHash(hashControlKey("rck_x")); err != nil {
		t.Fatal(err)
	}

	// loopback 来源直接打 reset(无 Bearer,靠豁免)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/core", strings.NewReader(`{"action":"reset"}`))
	r.RemoteAddr = "127.0.0.1:40000"
	g.handleCore(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reset: code = %d, body = %s", w.Code, w.Body.String())
	}

	if _, err := os.Stat(defaultConfigPath()); !os.IsNotExist(err) {
		t.Fatal("k2r.yml must be removed")
	}
	if loadState() != nil {
		t.Fatal("state.json must be removed")
	}
	if g.currentControlKeyHash() != "" {
		t.Fatal("in-memory control key must be cleared")
	}
	if loadControlKeyHash() != "" {
		t.Fatal("control-key.json must be removed")
	}
}

// TestGatewayRewriteRouteVia mirrors daemon's TestRewriteRouteVia (same
// pure-function shape, gateway's own config.ClientConfig usage) — the
// building block persistRotatedCredential uses to patch only the rotated
// route in an existing on-disk config.
func TestGatewayRewriteRouteVia(t *testing.T) {
	oldVia := "k2subs://u:oldtok@center.example/api/subs"
	newVia := "k2subs://u:newtok@center.example/api/subs"

	t.Run("RewritesMatchingRoute", func(t *testing.T) {
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{
			{Via: "direct"},
			{Via: oldVia},
		}}
		if !rewriteRouteVia(cfg, oldVia, newVia) {
			t.Fatal("want rewrite to report true")
		}
		if cfg.Routes[1].Via != newVia {
			t.Fatalf("route not rewritten: %q", cfg.Routes[1].Via)
		}
		if cfg.Routes[0].Via != "direct" {
			t.Fatal("unrelated route must not change")
		}
	})

	t.Run("NoMatch_False", func(t *testing.T) {
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{{Via: "k2v5://x@h:443"}}}
		if rewriteRouteVia(cfg, oldVia, newVia) {
			t.Fatal("no matching route — must report false")
		}
	})

	t.Run("NilAndDegenerate_False", func(t *testing.T) {
		if rewriteRouteVia(nil, oldVia, newVia) {
			t.Fatal("nil cfg must be a no-op")
		}
		cfg := &config.ClientConfig{Routes: []config.RouteConfig{{Via: oldVia}}}
		if rewriteRouteVia(cfg, oldVia, oldVia) {
			t.Fatal("identical old/new must be a no-op")
		}
	})
}

// TestPersistRotatedCredentialWritesBothStores：首次设置(磁盘上尚无 k2r.yml)
// 时的落盘回退路径必须复用 applyCredential（k2r.yml + state.json 双写，spec
// §4.3 P1）。
func TestPersistRotatedCredentialWritesBothStores(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	g := &Gateway{}
	oldRaw := "k2subs://router-x:tok123@center.example/api/subs"
	newRaw := "k2subs://router-x:rotated-tok@center.example/api/subs"
	// No k2r.yml on disk yet: LoadGateway fails → first-ever-setup fallback
	// (applyCredential) must still persist something.
	g.persistRotatedCredential(oldRaw, newRaw)

	st := loadState()
	if st == nil || st.Config == nil || len(st.Config.Routes) == 0 {
		t.Fatal("state.json not persisted after rotation")
	}
	if st.Config.Routes[0].Via != newRaw {
		t.Fatalf("state route = %q, want rotated URL", st.Config.Routes[0].Via)
	}
	data, err := os.ReadFile(filepath.Join(dir, "k2r.yml"))
	if err != nil {
		t.Fatalf("k2r.yml not persisted: %v", err)
	}
	if !strings.Contains(string(data), "rotated-tok") {
		t.Fatal("k2r.yml must carry the rotated credential")
	}
}

// TestPersistRotatedCredentialPreservesOtherConfig is the regression test
// for whole-branch review Finding #2: an existing on-disk k2r.yml carries
// operator-set fields (lan_subnets, max_lan_client, listen_port) alongside
// the route to rotate. Before the fix, persistRotatedCredential reused
// applyCredential → GatewayFromURL, which builds a bare single-route config
// from scratch and PersistConfig would overwrite k2r.yml wholesale, silently
// wiping all of these (max_lan_client in particular becomes 0 = unlimited,
// unenforced quota). After the fix, only the rotated route's Via changes —
// everything else must survive untouched.
func TestPersistRotatedCredentialPreservesOtherConfig(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	oldRaw := "k2subs://router-x:tok123@center.example/api/subs"
	newRaw := "k2subs://router-x:rotated-tok@center.example/api/subs"

	seed := GatewayFromURL(oldRaw)
	seed.LANSubnets = []string{"10.77.0.0/24"}
	seed.MaxLanClient = 7
	seed.ListenPort = 23456
	if err := PersistConfig(seed); err != nil {
		t.Fatalf("seed PersistConfig: %v", err)
	}

	g := &Gateway{}
	g.persistRotatedCredential(oldRaw, newRaw)

	cfg, err := LoadGateway(defaultConfigPath())
	if err != nil {
		t.Fatalf("LoadGateway after rotation: %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Via != newRaw {
		t.Fatalf("route not rewritten: %+v", cfg.Routes)
	}
	if len(cfg.LANSubnets) != 1 || cfg.LANSubnets[0] != "10.77.0.0/24" {
		t.Fatalf("lan_subnets wiped by rotation: %+v, want [10.77.0.0/24]", cfg.LANSubnets)
	}
	if cfg.MaxLanClient != 7 {
		t.Fatalf("max_lan_client wiped by rotation: got %d, want 7", cfg.MaxLanClient)
	}
	if cfg.ListenPort != 23456 {
		t.Fatalf("listen_port wiped by rotation: got %d, want 23456", cfg.ListenPort)
	}

	st := loadState()
	if st == nil || st.Config == nil {
		t.Fatal("state.json not persisted after rotation")
	}
	if len(st.Config.Routes) != 1 || st.Config.Routes[0].Via != newRaw {
		t.Fatalf("state route not rewritten: %+v", st.Config.Routes)
	}
	if st.Config.MaxLanClient != 7 {
		t.Fatalf("state max_lan_client wiped: got %d, want 7", st.Config.MaxLanClient)
	}
}

// TestPersistRotatedCredentialNoMatchDoesNotCorrupt: when oldRaw doesn't
// match any route in the existing on-disk config (e.g. the callback fires
// for a subscription this gateway process isn't currently tracking, or the
// on-disk config was already replaced by a subsequent set-credential), the
// existing k2r.yml must be left byte-for-byte untouched — no partial/wrong
// rewrite. Mirrors daemon's TestPersistRotatedCreds_NoMatch_DoesNotPersist.
func TestPersistRotatedCredentialNoMatchDoesNotCorrupt(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	seed := GatewayFromURL("k2subs://router-x:tok123@center.example/api/subs")
	if err := PersistConfig(seed); err != nil {
		t.Fatalf("seed PersistConfig: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "k2r.yml"))
	if err != nil {
		t.Fatalf("read seeded k2r.yml: %v", err)
	}

	g := &Gateway{}
	g.persistRotatedCredential(
		"k2subs://router-x:othertok@other.example/api/subs",
		"k2subs://router-x:othernewtok@other.example/api/subs",
	)

	after, err := os.ReadFile(filepath.Join(dir, "k2r.yml"))
	if err != nil {
		t.Fatalf("read k2r.yml after no-match rotation: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("k2r.yml changed on a no-match rotation:\nbefore=%s\nafter=%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state.json should not exist after a no-match rotation, stat err = %v", err)
	}
}
