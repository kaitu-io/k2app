package center

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/redis"
)

// =====================================================================
// TestExtractSubsBasicAuth — table-driven unit tests for Basic Auth parsing
// =====================================================================

func TestExtractSubsBasicAuth(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		wantOK    bool
		wantUDID  string
		wantToken string
	}{
		{
			name:      "valid credentials",
			header:    "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:mytoken")),
			wantOK:    true,
			wantUDID:  "myudid",
			wantToken: "mytoken",
		},
		{
			name:   "Bearer token (not Basic)",
			header: "Bearer sometoken",
			wantOK: false,
		},
		{
			name:   "empty header",
			header: "",
			wantOK: false,
		},
		{
			name:   "no colon in payload",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte("nocolonhere")),
			wantOK: false,
		},
		{
			name:   "empty password (user:)",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:")),
			wantOK: false,
		},
		{
			name:   "empty username (:token)",
			header: "Basic " + base64.StdEncoding.EncodeToString([]byte(":mytoken")),
			wantOK: false,
		},
		{
			name:      "colon in token is ok (udid:tok:en)",
			header:    "Basic " + base64.StdEncoding.EncodeToString([]byte("myudid:tok:en")),
			wantOK:    true,
			wantUDID:  "myudid",
			wantToken: "tok:en",
		},
	}

	gin.SetMode(gin.TestMode)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/", nil)
			if tc.header != "" {
				c.Request.Header.Set("Authorization", tc.header)
			}

			udid, token, ok := extractSubsBasicAuth(c)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantUDID, udid)
				assert.Equal(t, tc.wantToken, token)
			}
		})
	}
}

// =====================================================================
// TestInjectSubsCreds — table-driven unit tests for credential injection
// =====================================================================

func TestInjectSubsCreds(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		udid      string
		token     string
		want      string
	}{
		{
			name:      "k2v5 URL injects credentials before host",
			serverURL: "k2v5://host.example.com:443?ech=x",
			udid:      "myudid",
			token:     "mytoken",
			want:      "k2v5://myudid:mytoken@host.example.com:443?ech=x",
		},
		{
			name:      "no scheme separator passes through unchanged",
			serverURL: "host.example.com:443",
			udid:      "myudid",
			token:     "mytoken",
			want:      "host.example.com:443",
		},
		{
			name:      "empty string passes through unchanged",
			serverURL: "",
			udid:      "myudid",
			token:     "mytoken",
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := injectSubsCreds(tc.serverURL, tc.udid, tc.token)
			assert.Equal(t, tc.want, got)
		})
	}
}

// =====================================================================
// TestApiSubs_NoAuth_Returns401 — handler test: missing auth → raw HTTP 401
//
// /api/subs is an external-protocol (k2subs://) wire endpoint. Unlike the rest
// of /api/*, it returns real HTTP status codes with plain-text body hints,
// NOT the {code, message, data} envelope — daemon (k2/config/subscription.go)
// formats errors as `subscription fetch: status %d: %s` using the body as hint.
// =====================================================================

func TestApiSubs_NoAuth_Returns401(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/api/subs", api_subs)

	req, err := http.NewRequest("GET", "/api/subs", nil)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing credentials")

	// Body must NOT be JSON-wrapped in the Center {code, message, data} envelope.
	// Daemon parses the body as a plain hint string.
	assert.NotContains(t, w.Body.String(), `"code":`)
	assert.NotContains(t, w.Body.String(), `"data":`)
}

// =====================================================================
// TestApiSubs_MalformedAuth_ReturnsRaw401 — malformed Basic Auth → raw 401
// =====================================================================

func TestApiSubs_MalformedAuth_ReturnsRaw401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/subs", api_subs)

	cases := []struct {
		name   string
		header string
	}{
		{"bearer instead of basic", "Bearer abc"},
		{"empty password", "Basic " + base64.StdEncoding.EncodeToString([]byte("udid:"))},
		{"garbage base64", "Basic !!!not-base64!!!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/api/subs", nil)
			req.Header.Set("Authorization", tc.header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), "missing credentials")
			// No JSON envelope leak.
			assert.NotContains(t, w.Body.String(), `"code"`)
		})
	}
}

// =====================================================================
// applyPenaltyWeights tests — verify Redis-backed per-request rotation
//
// These tests rely on testInitConfig() spinning up miniredis and pointing
// qtoolkit/redis at it (see testutil_test.go). Each test wipes the
// subs:penalty:* keyspace via testMiniRedis.FlushAll() on entry so state
// from other tests doesn't leak.
// =====================================================================

// subsTestCtx returns a gin.Context suitable for passing to applyPenaltyWeights.
func subsTestCtx() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("GET", "/", nil)
	return c
}

// readPenalty returns the current Redis-stored factor for a tunnel, or
// (1.0, false) if the key is absent.
func readPenalty(t *testing.T, id uint64) (float64, bool) {
	t.Helper()
	var f float64
	exists, err := redis.CacheGet(fmt.Sprintf("subs:penalty:%d", id), &f)
	require.NoError(t, err)
	if !exists {
		return 1.0, false
	}
	return f, true
}

func TestApplyPenaltyWeights_ColdStart(t *testing.T) {
	testInitConfig()
	testMiniRedis.FlushAll()

	ids := []uint64{1, 2, 3, 4}
	items := []SubsTunnel{
		{URL: "k2v5://a", Weight: 1},
		{URL: "k2v5://b", Weight: 1},
		{URL: "k2v5://c", Weight: 1},
		{URL: "k2v5://d", Weight: 1},
	}

	applyPenaltyWeights(subsTestCtx(), ids, items)

	// Exactly one tunnel should be penalized (factor=0.5 → scaled weight 500).
	// The rest stay at base * subsWeightScale = 1000.
	penalizedCount := 0
	for _, it := range items {
		if it.Weight == 500 {
			penalizedCount++
		} else {
			assert.Equal(t, 1000, it.Weight, "non-penalized tunnel should have weight 1000")
		}
	}
	assert.Equal(t, 1, penalizedCount, "exactly one tunnel should be penalized on cold start")

	// Redis should have exactly one penalty key with value 0.5 and TTL close to 60s.
	penalizedKeys := 0
	for _, id := range ids {
		if f, exists := readPenalty(t, id); exists {
			penalizedKeys++
			assert.InDelta(t, 0.5, f, 1e-9)
			ttl := testMiniRedis.TTL(fmt.Sprintf("subs:penalty:%d", id))
			assert.InDelta(t, 60, ttl.Seconds(), 2, "TTL should be ~60s")
		}
	}
	assert.Equal(t, 1, penalizedKeys)
}

func TestApplyPenaltyWeights_FiveRequestsRotate(t *testing.T) {
	testInitConfig()
	testMiniRedis.FlushAll()

	ids := []uint64{1, 2, 3, 4}
	seenTops := make(map[uint64]int)

	for i := 0; i < 5; i++ {
		items := []SubsTunnel{
			{URL: "a", Weight: 1},
			{URL: "b", Weight: 1},
			{URL: "c", Weight: 1},
			{URL: "d", Weight: 1},
		}
		applyPenaltyWeights(subsTestCtx(), ids, items)

		// Find which tunnel got penalized this round by comparing against the
		// prior factor. Simpler: the tunnel with the *lowest* weight this request
		// may not be the newly-penalized one if multiple are already down. We
		// instead track by reading Redis state changes across the loop.
		for j, it := range items {
			_ = j
			_ = it
		}
	}

	// After 5 requests, Redis should have 3+ distinct penalty keys (each pick
	// targets the then-highest effective tunnel; with 4 equal-base tunnels
	// cascading at x0.5, at minimum 3 unique IDs get penalized across 5 calls).
	for _, id := range ids {
		if _, exists := readPenalty(t, id); exists {
			seenTops[id]++
		}
	}
	assert.GreaterOrEqual(t, len(seenTops), 3,
		"5 cascading requests should distribute penalties across >= 3 tunnels")
}

func TestApplyPenaltyWeights_HeterogeneousBase(t *testing.T) {
	testInitConfig()
	testMiniRedis.FlushAll()

	ids := []uint64{1, 2, 3}
	// Base weights: [10, 1, 1]. Tunnel 1 should absorb most penalties early
	// until its effective weight drops into the same band as 2 and 3.
	bigBaseHitCount := 0
	const rounds = 10

	for i := 0; i < rounds; i++ {
		items := []SubsTunnel{
			{URL: "big", Weight: 10},
			{URL: "sm1", Weight: 1},
			{URL: "sm2", Weight: 1},
		}
		applyPenaltyWeights(subsTestCtx(), ids, items)
	}

	// After 10 rounds, check Redis state to infer hit distribution.
	// Tunnel 1 should have been penalized many times (factor multiplied by 0.5
	// each hit). Compute approximate hit count from its stored factor:
	// factor = 0.5 ^ hits → hits = log2(1/factor).
	f1, exists := readPenalty(t, 1)
	require.True(t, exists, "high-base tunnel should have been penalized")
	// After ~4 hits factor=0.0625 → eff=0.625, which is below base=1 for sm1/sm2.
	// From there, small tunnels start winning ties. So expect >= 4 hits on tunnel 1.
	bigBaseHitCount = 0
	for f := f1; f < 0.99; f *= 2 {
		bigBaseHitCount++
	}
	assert.GreaterOrEqual(t, bigBaseHitCount, 4,
		"big-base tunnel should absorb >= 4 penalties before yielding, got %d (factor=%v)", bigBaseHitCount, f1)
}

func TestApplyPenaltyWeights_RedisDown(t *testing.T) {
	testInitConfig()

	// Stop miniredis entirely — qtoolkit/redis calls will now return errors.
	testMiniRedis.Close()
	t.Cleanup(func() {
		// Restart for any subsequent tests sharing the suite.
		_ = testMiniRedis.Restart()
	})

	ids := []uint64{100, 101, 102}
	items := []SubsTunnel{
		{URL: "a", Weight: 1},
		{URL: "b", Weight: 3},
		{URL: "c", Weight: 2},
	}

	// Must not panic and must leave each item with base*scale weight.
	assert.NotPanics(t, func() {
		applyPenaltyWeights(subsTestCtx(), ids, items)
	})

	assert.Equal(t, 1000, items[0].Weight)
	assert.Equal(t, 3000, items[1].Weight)
	assert.Equal(t, 2000, items[2].Weight)
}

func TestApplyPenaltyWeights_TTLExpiry(t *testing.T) {
	testInitConfig()
	testMiniRedis.FlushAll()

	ids := []uint64{1, 2}
	items := []SubsTunnel{
		{URL: "a", Weight: 1},
		{URL: "b", Weight: 1},
	}
	applyPenaltyWeights(subsTestCtx(), ids, items)

	// Exactly one of {1, 2} is now penalized.
	anyPenalized := false
	for _, id := range ids {
		if _, exists := readPenalty(t, id); exists {
			anyPenalized = true
		}
	}
	require.True(t, anyPenalized)

	// Fast-forward past TTL.
	testMiniRedis.FastForward(61 * 1e9) // 61s in nanoseconds → miniredis uses time.Duration

	for _, id := range ids {
		_, exists := readPenalty(t, id)
		assert.False(t, exists, "tunnel %d penalty key should have expired", id)
	}

	// Next call should start fresh: one tunnel again penalized to 0.5.
	items2 := []SubsTunnel{
		{URL: "a", Weight: 1},
		{URL: "b", Weight: 1},
	}
	applyPenaltyWeights(subsTestCtx(), ids, items2)
	penalizedAfter := 0
	for _, id := range ids {
		if f, exists := readPenalty(t, id); exists {
			assert.InDelta(t, 0.5, f, 1e-9, "factor should be 0.5 after first hit post-expiry")
			penalizedAfter++
		}
	}
	assert.Equal(t, 1, penalizedAfter)
}

func TestApplyPenaltyWeights_Concurrent(t *testing.T) {
	testInitConfig()
	testMiniRedis.FlushAll()

	ids := []uint64{1, 2, 3, 4}

	const workers = 10
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			items := []SubsTunnel{
				{URL: "a", Weight: 1},
				{URL: "b", Weight: 1},
				{URL: "c", Weight: 1},
				{URL: "d", Weight: 1},
			}
			applyPenaltyWeights(subsTestCtx(), ids, items)
		}()
	}
	wg.Wait()

	// At least one tunnel should be penalized. Concurrency may mean some
	// requests race on the same top and overwrite; that's the "误差不影响" case.
	anyPenalized := false
	for _, id := range ids {
		if f, exists := readPenalty(t, id); exists && f <= 0.5+1e-9 {
			anyPenalized = true
		}
	}
	assert.True(t, anyPenalized, "at least one tunnel should carry a penalty after 10 concurrent requests")
}

func TestWriteSubsOK_SetsCacheControlHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", func(c *gin.Context) {
		writeSubsOK(c, SubsResponse{
			Tunnels: []SubsTunnel{{URL: "k2v5://x", Weight: 1000}},
			Refresh: 1800,
		})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store, private", w.Header().Get("Cache-Control"),
		"successful /api/subs responses must disable caching end-to-end")
	assert.Contains(t, w.Body.String(), `"url":"k2v5://x"`)
}
