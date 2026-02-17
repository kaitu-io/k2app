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
