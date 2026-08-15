//go:build linux

package webui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aead.dev/minisign"
)

const testManifestJSON = `{
	"version": "0.4.9.1234",
	"url": "0.4.9.1234/web.zip",
	"hash": "sha256:deadbeef",
	"size": 42,
	"sig": "",
	"min_linux": "0.4.9",
	"min_bridge": 1,
	"released_at": "2026-08-14T12:00:00Z",
	"min_native": "0.4.8"
}`

func TestFetchManifestFallsBackToSecondEndpoint(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kaitu/web/latest.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(testManifestJSON))
	}))
	defer good.Close()

	o := NewWebOTA(WebOTAConfig{
		Endpoints:    []string{bad.URL + "/kaitu", good.URL + "/kaitu"},
		Dir:          t.TempDir(),
		ShellVersion: "0.4.9",
	})
	m, endpoint, err := o.fetchManifest()
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if endpoint != good.URL+"/kaitu" {
		t.Errorf("endpoint = %q, want the good one", endpoint)
	}
	// 断言解析后的字段值（不要对原始 JSON 字符串断言 —— Go 会把 & 转义成 &）
	if m.Version != "0.4.9.1234" || m.URL != "0.4.9.1234/web.zip" ||
		m.MinLinux != "0.4.9" || m.MinBridge != 1 || m.Size != 42 {
		t.Errorf("manifest fields wrong: %+v", m)
	}
}

func TestFetchManifestAllEndpointsFail(t *testing.T) {
	bad := httptest.NewServer(http.NotFoundHandler())
	defer bad.Close()
	o := NewWebOTA(WebOTAConfig{Endpoints: []string{bad.URL + "/kaitu"}, Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if _, _, err := o.fetchManifest(); err == nil {
		t.Fatal("expected error when all endpoints fail")
	}
}

func TestLocalVersionFallsBackToShellVersion(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if v := o.localVersion(); v != "0.4.9" {
		t.Errorf("localVersion = %q, want shell fallback 0.4.9", v)
	}
}

func TestLocalVersionReadsVersionTxt(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	if err := os.MkdirAll(o.CurrentDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.CurrentDir(), "version.txt"), []byte("0.4.9.1000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if v := o.localVersion(); v != "0.4.9.1000" {
		t.Errorf("localVersion = %q, want 0.4.9.1000", v)
	}
}

func TestShouldApplyGates(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	cases := []struct {
		name string
		m    WebOTAManifest
		want bool
	}{
		{"newer passes", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.4.9", MinBridge: 1}, true},
		{"min_linux blocks", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.5.0", MinBridge: 1}, false},
		{"min_bridge blocks", WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.4.9", MinBridge: LinuxBridgeVersion + 1}, false},
		{"not newer blocks", WebOTAManifest{Version: "0.4.9", MinLinux: "", MinBridge: 0}, false},
		{"empty gates pass", WebOTAManifest{Version: "0.4.9.1", MinLinux: "", MinBridge: 0}, true},
	}
	for _, tc := range cases {
		if got, reason := o.shouldApply(&tc.m); got != tc.want {
			t.Errorf("%s: shouldApply = %v (%s), want %v", tc.name, got, reason, tc.want)
		}
	}
}

// --- apply pipeline ---

// makeZip builds an in-memory zip whose top level is dist content.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// testKeyPair generates a minisign key pair and returns the base64 key
// line of the public key (last line of its text encoding) + private key.
func testKeyPair(t *testing.T) (string, minisign.PrivateKey) {
	t.Helper()
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	text, err := pub.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	return lines[len(lines)-1], priv
}

func TestVerifySHA256(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	data := []byte("hello web ota")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifySHA256(p, "sha256:"+sha256Hex(data)); err != nil {
		t.Errorf("good hash rejected: %v", err)
	}
	if err := verifySHA256(p, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Error("bad hash accepted")
	}
	if err := verifySHA256(p, sha256Hex(data)); err == nil {
		t.Error("hash without sha256: prefix accepted")
	}
}

func TestVerifyMinisign(t *testing.T) {
	pubB64, priv := testKeyPair(t)
	p := filepath.Join(t.TempDir(), "f")
	data := []byte("signed bundle bytes")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(minisign.Sign(priv, data))

	if err := verifyMinisign(p, sig, pubB64); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
	if err := verifyMinisign(p, "", pubB64); err == nil {
		t.Error("empty signature accepted")
	}
	// 换内容 → 验签必须失败
	if err := os.WriteFile(p, []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyMinisign(p, sig, pubB64); err == nil {
		t.Error("signature over tampered content accepted")
	}
	// 换公钥 → 验签必须失败
	otherPub, _ := testKeyPair(t)
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyMinisign(p, sig, otherPub); err == nil {
		t.Error("signature accepted under wrong public key")
	}
}

func TestUnzipToRejectsZipSlip(t *testing.T) {
	evil := makeZip(t, map[string]string{"../evil.txt": "escape"})
	zp := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(zp, evil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zp, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("zip-slip entry accepted")
	}
}

func TestUnzipToExtractsNestedFiles(t *testing.T) {
	z := makeZip(t, map[string]string{
		"index.html":    "<html></html>",
		"assets/app.js": "js",
	})
	zp := filepath.Join(t.TempDir(), "web.zip")
	if err := os.WriteFile(zp, z, 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if err := unzipTo(zp, out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "assets/app.js"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
}

func TestSwapRotatesCurrentToPrevious(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	mustWriteDir := func(name, marker string) {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "index.html"), []byte(marker), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteDir("current", "v1")
	mustWriteDir("pending", "v2")

	if err := o.swap(filepath.Join(dir, "pending")); err != nil {
		t.Fatal(err)
	}
	cur, err := os.ReadFile(filepath.Join(dir, "current", "index.html"))
	if err != nil || string(cur) != "v2" {
		t.Errorf("current = %q, %v; want v2", cur, err)
	}
	prev, err := os.ReadFile(filepath.Join(dir, "previous", "index.html"))
	if err != nil || string(prev) != "v1" {
		t.Errorf("previous = %q, %v; want v1", prev, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pending")); !os.IsNotExist(err) {
		t.Error("pending should be gone after swap")
	}
}

func TestApplyEndToEnd(t *testing.T) {
	pubB64, priv := testKeyPair(t)
	zipData := makeZip(t, map[string]string{
		"index.html":    "<html><head></head><body>NEW</body></html>",
		"assets/app.js": "js",
	})
	sig := base64.StdEncoding.EncodeToString(minisign.Sign(priv, zipData))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/kaitu/web/0.4.9.1234/web.zip" {
			w.Write(zipData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{
		Endpoints:    []string{srv.URL + "/kaitu"},
		Dir:          dir,
		ShellVersion: "0.4.9",
		PublicKey:    pubB64,
	})
	m := &WebOTAManifest{
		Version: "0.4.9.1234",
		URL:     "0.4.9.1234/web.zip",
		Hash:    "sha256:" + sha256Hex(zipData),
		Size:    int64(len(zipData)),
		Sig:     sig,
	}
	if err := o.apply(m, srv.URL+"/kaitu"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	idx, err := os.ReadFile(filepath.Join(o.CurrentDir(), "index.html"))
	if err != nil || !strings.Contains(string(idx), "NEW") {
		t.Errorf("current/index.html = %q, %v", idx, err)
	}
	if v := o.localVersion(); v != "0.4.9.1234" {
		t.Errorf("localVersion after apply = %q, want 0.4.9.1234", v)
	}

	// 坏 hash 拒绝，且不动 current
	bad := *m
	bad.Version = "0.4.9.1235"
	bad.Hash = "sha256:" + strings.Repeat("0", 64)
	if err := o.apply(&bad, srv.URL+"/kaitu"); err == nil {
		t.Fatal("bad hash accepted")
	}
	if v := o.localVersion(); v != "0.4.9.1234" {
		t.Errorf("current was touched by failed apply: %q", v)
	}

	// 坏签名拒绝
	badSig := *m
	badSig.Version = "0.4.9.1236"
	badSig.Sig = base64.StdEncoding.EncodeToString(minisign.Sign(priv, []byte("other")))
	if err := o.apply(&badSig, srv.URL+"/kaitu"); err == nil {
		t.Fatal("bad signature accepted")
	}

	// bundle 缺 index.html 拒绝
	noIdxZip := makeZip(t, map[string]string{"assets/app.js": "js"})
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(noIdxZip)
	}))
	defer srv2.Close()
	noIdx := &WebOTAManifest{
		Version: "0.4.9.1237",
		URL:     "0.4.9.1237/web.zip",
		Hash:    "sha256:" + sha256Hex(noIdxZip),
		Sig:     base64.StdEncoding.EncodeToString(minisign.Sign(priv, noIdxZip)),
	}
	if err := o.apply(noIdx, srv2.URL); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("bundle without index.html: err = %v, want index.html error", err)
	}
}

func TestCheckOnceFullCycle(t *testing.T) {
	pubB64, priv := testKeyPair(t)
	zipData := makeZip(t, map[string]string{"index.html": "<html><head></head><body>CYCLE</body></html>"})
	sig := base64.StdEncoding.EncodeToString(minisign.Sign(priv, zipData))

	mux := http.NewServeMux()
	mux.HandleFunc("/kaitu/web/latest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WebOTAManifest{
			Version:   "0.4.9.1234",
			URL:       "0.4.9.1234/web.zip",
			Hash:      "sha256:" + sha256Hex(zipData),
			Size:      int64(len(zipData)),
			Sig:       sig,
			MinLinux:  "0.4.9",
			MinBridge: 1,
		})
	})
	mux.HandleFunc("/kaitu/web/0.4.9.1234/web.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	o := NewWebOTA(WebOTAConfig{
		Endpoints:    []string{srv.URL + "/kaitu"},
		Dir:          t.TempDir(),
		ShellVersion: "0.4.9",
		PublicKey:    pubB64,
	})
	if err := o.checkOnce(); err != nil {
		t.Fatalf("checkOnce: %v", err)
	}
	if v := o.localVersion(); v != "0.4.9.1234" {
		t.Errorf("localVersion = %q, want 0.4.9.1234", v)
	}
	// 再跑一次：已是最新 → 不 apply、不报错
	if err := o.checkOnce(); err != nil {
		t.Fatalf("second checkOnce should be a no-op, got %v", err)
	}
}

func TestStartStopsOnContextCancel(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{
		Endpoints:    []string{"http://127.0.0.1:1"}, // 不可达；循环只须能停
		Dir:          t.TempDir(),
		ShellVersion: "0.4.9",
		InitialDelay: time.Millisecond,
		Interval:     time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	o.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond) // goroutine 退出即可；泄漏由 goleak 类工具兜底，此处不额外断言
}
