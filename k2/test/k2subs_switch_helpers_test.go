//go:build !release

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/subscription"
	"github.com/kaitu-io/k2/wire"

	"go.uber.org/goleak"
)

// ---------------------------------------------------------------------------
// fakeSubsServer — hot-mutable httptest.NewTLSServer for /api/subs
// ---------------------------------------------------------------------------

type fakeSubsServer struct {
	srv *httptest.Server

	mu       sync.Mutex
	tunnels  []config.Tunnel
	refresh  *int
	queryLog []string

	fetches  atomic.Int64
	failNext atomic.Bool
	failAll  atomic.Bool
}

func newFakeSubsServer(t *testing.T) *fakeSubsServer {
	t.Helper()
	f := &fakeSubsServer{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSubsServer) handle(w http.ResponseWriter, r *http.Request) {
	f.fetches.Add(1)

	f.mu.Lock()
	f.queryLog = append(f.queryLog, r.URL.RawQuery)
	tunnels := append([]config.Tunnel(nil), f.tunnels...)
	refresh := f.refresh
	f.mu.Unlock()

	if f.failAll.Load() {
		http.Error(w, "maintenance", http.StatusServiceUnavailable)
		return
	}
	if f.failNext.Swap(false) {
		http.Error(w, "transient", http.StatusServiceUnavailable)
		return
	}

	type resp struct {
		Tunnels []config.Tunnel `json:"tunnels"`
		Refresh *int            `json:"refresh,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp{Tunnels: tunnels, Refresh: refresh})
}

func (f *fakeSubsServer) SetTunnels(tunnels []config.Tunnel) {
	f.mu.Lock()
	f.tunnels = tunnels
	f.mu.Unlock()
}

func (f *fakeSubsServer) SetRefresh(secs int) {
	f.mu.Lock()
	if secs < 0 {
		f.refresh = nil
	} else {
		f.refresh = &secs
	}
	f.mu.Unlock()
}

func (f *fakeSubsServer) FailNext()                { f.failNext.Store(true) }
func (f *fakeSubsServer) FailAll(on bool)          { f.failAll.Store(on) }
func (f *fakeSubsServer) Fetches() int64           { return f.fetches.Load() }
func (f *fakeSubsServer) HTTPClient() *http.Client { return f.srv.Client() }

func (f *fakeSubsServer) QuerySnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.queryLog))
	copy(out, f.queryLog)
	return out
}

// SubsURL builds k2subs://creds@host/api?extraQuery.
func (f *fakeSubsServer) SubsURL(creds, extraQuery string) string {
	host := strings.TrimPrefix(f.srv.URL, "https://")
	u := "k2subs://" + creds + "@" + host + "/api"
	if extraQuery != "" {
		u += "?" + extraQuery
	}
	return u
}

// ---------------------------------------------------------------------------
// fakeWireSetup — programmable replacement for engine.setupOutboundTransportFn
// ---------------------------------------------------------------------------

type fakeWireSetup struct {
	mu       sync.Mutex
	failURLs map[string]error
	slowURLs map[string]time.Duration
	setupLog []string
	builtTMs map[string]*wire.TransportManager
}

func newFakeWireSetup() *fakeWireSetup {
	return &fakeWireSetup{
		failURLs: map[string]error{},
		slowURLs: map[string]time.Duration{},
		builtTMs: map[string]*wire.TransportManager{},
	}
}

func (f *fakeWireSetup) FailURL(url string, err error) {
	f.mu.Lock()
	f.failURLs[url] = err
	f.mu.Unlock()
}

func (f *fakeWireSetup) SlowURL(url string, d time.Duration) {
	f.mu.Lock()
	f.slowURLs[url] = d
	f.mu.Unlock()
}

func (f *fakeWireSetup) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.setupLog))
	copy(out, f.setupLog)
	return out
}

func (f *fakeWireSetup) BuiltTM(url string) *wire.TransportManager {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builtTMs[url]
}

// Install replaces engine.setupOutboundTransportFn for the duration of t.
func (f *fakeWireSetup) Install(t *testing.T) {
	t.Helper()
	stub := func(ctx context.Context, via string, dd wire.DirectDialer, wireMode string, clientVersion string) (*engine.OutboundTransportShape, wire.Dialer, error) {
		f.mu.Lock()
		f.setupLog = append(f.setupLog, via)
		failErr := f.failURLs[via]
		slow := f.slowURLs[via]
		f.mu.Unlock()

		if slow > 0 {
			select {
			case <-time.After(slow):
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		if failErr != nil {
			return nil, nil, failErr
		}

		tm := wire.NewTransportManager()
		f.mu.Lock()
		f.builtTMs[via] = tm
		f.mu.Unlock()

		ot := engine.NewOutboundTransportForSetup(via, tm)
		return ot, nil, nil
	}
	prev := engine.SetSetupOutboundTransportFnShape(stub)
	t.Cleanup(func() {
		engine.SetSetupOutboundTransportFnShape(prev)
	})
}

// ---------------------------------------------------------------------------
// k2subsFixture — composite of all moving parts
// ---------------------------------------------------------------------------

type k2subsFixture struct {
	T         *testing.T
	Reg       *probe.Registry
	Mgr       *subscription.Manager
	Engine    *engine.Engine
	Target    *engine.OutboundTransportShape
	SubsSrv   *fakeSubsServer
	WireSetup *fakeWireSetup
	CacheDir  string
	subsURL   string
}

// newK2subsFixture builds a fully-wired fixture with `initialTunnels` as the
// fakeSubsServer's first response.
//
// The fixture registers a goleak.VerifyNone cleanup BEFORE any other cleanups
// so that — under t.Cleanup's LIFO order — goleak runs LAST, after the
// httptest server has been Close()d and all subscription refresh goroutines
// (if started) have exited. Tests should therefore call newK2subsFixture as
// their first non-trivial setup step.
func newK2subsFixture(t *testing.T, initialTunnels []config.Tunnel) *k2subsFixture {
	t.Helper()

	// Snapshot goroutines already alive at fixture setup so VerifyNone flags
	// only leaks introduced by THIS test. Without this, goroutines leaked by
	// earlier tests in the same `package test` binary (notably the pion/ice +
	// mdns + quic-go stack from k2p_e2e, whose recvLoops linger after the QUIC
	// layer closes) would be attributed to whichever k2subs test's goleak runs
	// next. IgnoreCurrent is the right tool here — a blanket function ignore
	// can't be used because the leaked set includes k2's own `safego.Go`
	// goroutines, and ignoring those would mask real k2subs leaks.
	ignoreExisting := goleak.IgnoreCurrent()
	// Register goleak first → runs last (LIFO) → catches leaks after
	// every other cleanup has settled.
	t.Cleanup(func() {
		goleak.VerifyNone(t,
			ignoreExisting,
			// HTTP/2 transport idle conn keepalive — released as the
			// httptest.Server's idle conns close. Sometimes lingers a few ms
			// after Close(); harmless.
			goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
			goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		)
	})

	srv := newFakeSubsServer(t)
	srv.SetTunnels(initialTunnels)

	ws := newFakeWireSetup()
	ws.Install(t)

	cacheDir := t.TempDir()
	reg := probe.NewRegistry(15 * time.Minute)
	mgr := subscription.NewManager(cacheDir, srv.HTTPClient(), reg)
	e := engine.New()
	subsURL := srv.SubsURL("alice:tok", "")

	return &k2subsFixture{
		T:         t,
		Reg:       reg,
		Mgr:       mgr,
		Engine:    e,
		SubsSrv:   srv,
		WireSetup: ws,
		CacheDir:  cacheDir,
		subsURL:   subsURL,
	}
}

func (f *k2subsFixture) SubsURL() string { return f.subsURL }

// Resolve runs Manager.Resolve, attaches a target outboundTransport for the
// resolved URL, and forces engine state to Connected. Returns the resolved URL.
func (f *k2subsFixture) Resolve(ctx context.Context) string {
	f.T.Helper()
	cfg := &config.ClientConfig{
		Routes: []config.RouteConfig{{Via: f.subsURL, Match: muxAll()}},
	}
	_, resolved, err := f.Mgr.Resolve(cfg)
	if err != nil {
		f.T.Fatalf("Resolve: %v", err)
	}
	url, ok := resolved[0]
	if !ok {
		f.T.Fatal("Resolve: route 0 not resolved")
	}

	target := engine.NewTestTarget(url)
	engine.AttachTestTarget(f.Engine, target)
	f.Engine.ForceConnected()
	f.Target = target

	f.T.Cleanup(f.Mgr.CloseAll)
	return url
}

// TriggerSwitch invokes engine.replaceOutboundServer (Phase-B swap) directly
// using the fixture's Manager as OutboundProvider.
func (f *k2subsFixture) TriggerSwitch(ctx context.Context, reason error) error {
	return engine.TriggerReplaceForTestCtx(ctx, f.Engine, f.Target, reason, nil, f.Mgr)
}

// ---------------------------------------------------------------------------
// shared helpers used by k2subs_switch_e2e_test.go
// ---------------------------------------------------------------------------

func muxAll() rule.MatchConfig { return rule.MatchConfig{All: true} }

// stripCreds returns the URL with userinfo removed: k2v5://host:port?... .
// Used by tests that want to bucket results without caring about creds.
func stripCreds(rawURL string) string {
	at := strings.Index(rawURL, "@")
	if at < 0 || !strings.HasPrefix(rawURL, "k2v5://") {
		return rawURL
	}
	return "k2v5://" + rawURL[at+1:]
}

// withCreds is the inverse of stripCreds: prepend userinfo to a bare
// k2v5://host:port URL. Lets tests reference both forms cleanly.
func withCreds(bareURL, creds string) string {
	if !strings.HasPrefix(bareURL, "k2v5://") {
		return bareURL
	}
	return "k2v5://" + creds + "@" + strings.TrimPrefix(bareURL, "k2v5://")
}

// mapScoreSource implements config.ScoreSource for deterministic Pick tests.
type mapScoreSource map[string]float64

func (m mapScoreSource) Score(url string) (float64, bool) {
	v, ok := m[url]
	return v, ok
}

// pickN runs n Pick calls, returning the URL histogram (creds stripped).
func pickN(t *testing.T, sub *config.Subscription, n int) map[string]int {
	t.Helper()
	out := map[string]int{}
	for i := 0; i < n; i++ {
		tun, err := sub.Pick(nil)
		if err != nil {
			t.Fatalf("iter %d: Pick: %v", i, err)
		}
		out[stripCreds(tun.URL)]++
	}
	return out
}

// neverSeenInPick asserts banned never appears in n Pick calls.
func neverSeenInPick(t *testing.T, sub *config.Subscription, banned string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		tun, err := sub.Pick(nil)
		if err != nil {
			t.Fatalf("iter %d: Pick: %v", i, err)
		}
		if stripCreds(tun.URL) == banned {
			t.Fatalf("iter %d: banned URL %q appeared", i, banned)
		}
	}
}

// errIs returns true if err's chain contains target or its message contains substr.
// Some callers use string matching when error wrapping isn't possible.
func errIs(err error, target error, substr string) bool {
	if err == nil {
		return false
	}
	if target != nil && errors.Is(err, target) {
		return true
	}
	return substr != "" && strings.Contains(err.Error(), substr)
}
