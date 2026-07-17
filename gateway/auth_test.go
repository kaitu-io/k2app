//go:build linux

package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestGateway(t *testing.T) *Gateway {
	t.Helper()
	withTempStateDir(t)
	g := &Gateway{}
	return g
}

func lanReq(method, path, body, bearer string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.RemoteAddr = "192.168.1.50:40000" // LAN 来源
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return r
}

func TestAuthorizedMatrix(t *testing.T) {
	g := newTestGateway(t)

	// 无绑定 key:LAN 一律 false,loopback true
	if g.authorized(lanReq("GET", "/api/log-level", "", "")) {
		t.Fatal("no key + LAN must be unauthorized")
	}
	loop := lanReq("GET", "/api/log-level", "", "")
	loop.RemoteAddr = "127.0.0.1:40000"
	if !g.authorized(loop) {
		t.Fatal("loopback must always be authorized")
	}

	// 绑定 key 后:正确 Bearer true,错误/缺失 false
	if err := g.setControlKeyHash(hashControlKey("rck_good")); err != nil {
		t.Fatal(err)
	}
	if !g.authorized(lanReq("GET", "/api/log-level", "", "rck_good")) {
		t.Fatal("correct bearer must pass")
	}
	if g.authorized(lanReq("GET", "/api/log-level", "", "rck_bad")) {
		t.Fatal("wrong bearer must fail")
	}
	if g.authorized(lanReq("GET", "/api/log-level", "", "")) {
		t.Fatal("missing bearer must fail")
	}
}

func TestRequireAuthMiddleware(t *testing.T) {
	g := newTestGateway(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := g.requireAuth(inner)

	// /ping 与 / 永远放行
	for _, p := range []string{"/ping", "/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, lanReq("GET", p, "", ""))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: code = %d, want 200", p, w.Code)
		}
	}
	// /api/core 透传给 handler(门控在 handleCore 内)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, lanReq("POST", "/api/core", `{}`, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("/api/core middleware must pass through, got %d", w.Code)
	}
	// 其他端点无 key → 401
	w = httptest.NewRecorder()
	h.ServeHTTP(w, lanReq("GET", "/api/log-level", "", ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("protected path without key: code = %d, want 401", w.Code)
	}
}

func TestHandleCoreTOFUGate(t *testing.T) {
	g := newTestGateway(t)

	// 无 key:set-credential 放行(走到参数校验,missing url → code 1 而非 401)
	w := httptest.NewRecorder()
	g.handleCore(w, lanReq("POST", "/api/core", `{"action":"set-credential","params":{}}`, ""))
	if w.Code == http.StatusUnauthorized {
		t.Fatal("TOFU: set-credential without key must not 401")
	}

	// 无 key:其他 action → 401
	w = httptest.NewRecorder()
	g.handleCore(w, lanReq("POST", "/api/core", `{"action":"status"}`, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status without key: code = %d, want 401", w.Code)
	}

	// 有 key:set-credential 也要求鉴权
	if err := g.setControlKeyHash(hashControlKey("rck_good")); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	g.handleCore(w, lanReq("POST", "/api/core", `{"action":"set-credential","params":{}}`, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("set-credential with bound key + no bearer: code = %d, want 401", w.Code)
	}
	// 正确 Bearer 的 version action 放行
	w = httptest.NewRecorder()
	g.handleCore(w, lanReq("POST", "/api/core", `{"action":"version"}`, "rck_good"))
	if w.Code != http.StatusOK {
		t.Fatalf("version with bearer: code = %d, want 200", w.Code)
	}
}
