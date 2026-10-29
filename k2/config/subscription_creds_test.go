package config

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchAdoptsRotatedCreds：响应 tunnel URL 携带同 udid 的新 token →
// 采纳（creds/raw/注入后的 tunnels 全部更新）+ 回调（锁外）携带新旧 raw。
func TestFetchAdoptsRotatedCreds(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://u:newtok@h.example.com:443/"}],"refresh":600}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	sub, err := NewSubscription("k2subs://u:oldtok@"+host+"/api/subs", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	var gotOld, gotNew string
	sub.SetOnCredsRotate(func(o, n string) { gotOld, gotNew = o, n })

	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := sub.Creds(); got != "u:newtok" {
		t.Fatalf("Creds = %q, want u:newtok", got)
	}
	if raw := sub.RawURL(); !strings.Contains(raw, "u:newtok@") {
		t.Fatalf("RawURL = %q, want rotated userinfo", raw)
	}
	if !strings.Contains(gotOld, "oldtok") || !strings.Contains(gotNew, "newtok") {
		t.Fatalf("callback old=%q new=%q, want oldtok→newtok", gotOld, gotNew)
	}
	// 注入后的 tunnel URL 必须带新凭据（下次连接即用新 token）。
	tunnels := sub.Tunnels()
	if len(tunnels) != 1 || !strings.Contains(tunnels[0].URL, "u:newtok@") {
		t.Fatalf("tunnels = %+v, want injected new creds", tunnels)
	}
}

// TestFetchIgnoresForeignUdidCreds：username（udid）不同 → 不采纳、不回调。
func TestFetchIgnoresForeignUdidCreds(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://other:newtok@h.example.com:443/"}]}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	sub, err := NewSubscription("k2subs://u:oldtok@"+host+"/api/subs", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	called := false
	sub.SetOnCredsRotate(func(string, string) { called = true })
	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sub.Creds() != "u:oldtok" || called {
		t.Fatalf("foreign-udid creds must not be adopted (creds=%q called=%v)", sub.Creds(), called)
	}
}

// TestFetchSameCredsNoRotate：响应回填的就是当前凭据 → 不回调（今天线上的
// 稳态；保证 Phase 0 铺开前每 30 分钟的刷新不制造落盘风暴）。
func TestFetchSameCredsNoRotate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tunnels":[{"url":"k2v5://u:oldtok@h.example.com:443/"}]}`)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")
	sub, err := NewSubscription("k2subs://u:oldtok@"+host+"/api/subs", "")
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	called := false
	sub.SetOnCredsRotate(func(string, string) { called = true })
	if err := sub.Fetch(context.Background(), srv.Client()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if called {
		t.Fatal("identical creds must not fire rotate callback")
	}
}
