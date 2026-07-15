package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// createApiCORSRouter creates a test router with ApiCORSMiddleware on /api group
func createApiCORSRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(ApiCORSMiddleware())
	api.OPTIONS("/*path", func(c *gin.Context) {})
	api.GET("/plans", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	return r
}

// createAppCORSRouter creates a test router with CORSMiddleware on /app group
func createAppCORSRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := r.Group("/app")
	app.Use(CORSMiddleware())
	app.GET("/tunnels", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})
	return r
}

func TestApiCORSMiddleware_LocalhostOriginAllowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://localhost:1420")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://localhost:1420", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_LoopbackOriginAllowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://127.0.0.1:1777")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://127.0.0.1:1777", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_RFC1918_10_Allowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://10.0.0.1")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://10.0.0.1", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_RFC1918_172_Allowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://172.16.0.1")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://172.16.0.1", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_RFC1918_192_Allowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://192.168.1.1")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://192.168.1.1", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_CapacitorOriginAllowed(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "capacitor://localhost")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "capacitor://localhost", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_TauriLocalhostAllowed(t *testing.T) {
	router := createApiCORSRouter()

	// Tauri v2 WebView2 on Windows uses http://tauri.localhost as origin
	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://tauri.localhost")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://tauri.localhost", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_SubdomainLocalhostAllowed(t *testing.T) {
	router := createApiCORSRouter()

	// Any *.localhost subdomain (RFC 6761 reserved TLD)
	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://foo.localhost:3000")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "http://foo.localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_PublicOriginRejected(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "https://evil.com")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestApiCORSMiddleware_PreflightReturns204(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("OPTIONS", "/api/plans", nil)
	req.Header.Set("Origin", "http://localhost:14580")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 204, w.Code)
	assert.Equal(t, "http://localhost:14580", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "X-CSRF-Token")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "X-K2-Client")
}

func TestApiCORSMiddleware_RFC1918_172_32_Rejected(t *testing.T) {
	router := createApiCORSRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	req.Header.Set("Origin", "http://172.32.0.1")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestAppCORSMiddleware_Unchanged(t *testing.T) {
	router := createAppCORSRouter()

	// kaitu.io should still be allowed on /app
	req, _ := http.NewRequest("GET", "/app/tunnels", nil)
	req.Header.Set("Origin", "https://www.kaitu.io")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "https://www.kaitu.io", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
}

// The headers webapp originates on every Center request. Sourced from
// webapp/src/services/cloud-api.ts — X-K2-Client (buildClientHeader) and
// X-K2-Brand (getBrandId, added in Phase 3). A preflight that omits any of
// them makes the browser block the real request, and the client sees a bare
// code:-1 with no server-side trace. Nothing else in the suite crosses this
// contract: the Go tests never run a browser, and the webapp tests mock fetch.
var webappOriginatedHeaders = []string{"Content-Type", "Authorization", "X-K2-Client", "X-K2-Brand"}

// /api is the cross-origin direct transport for every browser-context client
// we ship: Capacitor iOS/Android, the Tauri webview, and standalone dev. (The
// website is same-origin — api.ts resolves against window.location.host and is
// proxied by Next.js — so it never preflights.)
func TestApiCORSMiddleware_PreflightAllowsWebappHeaders(t *testing.T) {
	router := createApiCORSRouter()

	for _, origin := range []string{
		"capacitor://localhost",  // iOS
		"https://localhost",      // Android
		"http://tauri.localhost", // desktop webview
		"http://localhost:5173",  // dev
	} {
		t.Run(origin, func(t *testing.T) {
			req, _ := http.NewRequest("OPTIONS", "/api/plans", nil)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", "GET")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			allowed := w.Header().Get("Access-Control-Allow-Headers")
			assert.NotEmpty(t, allowed, "preflight returned no Access-Control-Allow-Headers for %s", origin)
			for _, h := range webappOriginatedHeaders {
				assert.Contains(t, allowed, h,
					"%s is missing from the CORS allow-list, so any webapp fetch that sends it fails preflight", h)
			}
		})
	}
}

// /app has no preflight path in production (the website is same-origin; direct
// /app access is WebSocket, which does not preflight), so this pins the header
// list on a plain allowed-origin response instead.
func TestAppCORSMiddleware_AllowsWebappHeaders(t *testing.T) {
	router := createAppCORSRouter()

	for _, origin := range BrandKaitu.Config().WebOrigins {
		t.Run(origin, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/app/tunnels", nil)
			req.Header.Set("Origin", origin)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			allowed := w.Header().Get("Access-Control-Allow-Headers")
			for _, h := range webappOriginatedHeaders {
				assert.Contains(t, allowed, h, "%s missing from the /app CORS allow-list", h)
			}
		})
	}
}
