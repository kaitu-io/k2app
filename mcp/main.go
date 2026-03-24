package main

import (
	"context"
	"log"
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
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "k2-mcp",
		Version: "0.1.0",
	}, nil)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
