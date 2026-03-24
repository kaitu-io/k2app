package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server represents a k2 VPN server entry.
type Server struct {
	ID                    int     `json:"id"`
	Name                  string  `json:"name"`
	Domain                string  `json:"domain"`
	Country               string  `json:"country"`
	Region                string  `json:"region"`
	TrafficUsagePercent   float64 `json:"traffic_usage_percent"`
	BandwidthUsagePercent float64 `json:"bandwidth_usage_percent"`
	ServerURL             string  `json:"server_url"`
}

// App holds shared state for all MCP tool handlers.
type App struct {
	center  *CenterClient
	daemon  *DaemonClient
	session *Session

	// Server list cache.
	serversMu       sync.RWMutex
	servers         []Server
	serversCachedAt time.Time
}

func main() {
	apiURL := envOr("KAITU_API_URL", "https://api.kaitu.io")
	daemonAddr := envOr("K2_DAEMON_ADDR", "127.0.0.1:1777")
	sessionDir := envOr("KAITU_SESSION_DIR", defaultSessionDir())

	sess := NewSession(sessionDir)
	if err := sess.Restore(); err != nil {
		log.Printf("session restore: %v", err)
	}

	center := NewCenterClient(apiURL)
	if sess.LoggedIn() {
		sess.mu.RLock()
		token := sess.AccessToken
		sess.mu.RUnlock()
		center.SetToken(token)
	}
	udid := sess.UDID()
	center.SetUDID(udid)

	app := &App{
		center:  center,
		daemon:  &DaemonClient{Addr: "http://" + daemonAddr},
		session: sess,
	}

	// Wire refresh source so 401 responses trigger automatic token refresh.
	app.center.SetRefreshSource(app.session)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "k2-mcp",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "login",
		Description: "Log in to Kaitu with email and password",
	}, app.toolLogin)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "account_info",
		Description: "Get current account information",
	}, app.toolAccountInfo)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_plans",
		Description: "List available subscription plans",
	}, app.toolListPlans)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "subscribe",
		Description: "Subscribe to a plan and get a payment URL",
	}, app.toolSubscribe)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_servers",
		Description: "List available VPN servers",
	}, app.toolListServers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "connect",
		Description: "Connect to a VPN server",
	}, app.toolConnect)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "disconnect",
		Description: "Disconnect from the VPN",
	}, app.toolDisconnect)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "status",
		Description: "Get current VPN connection status",
	}, app.toolStatus)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultSessionDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kaitu")
}
