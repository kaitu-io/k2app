package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server represents a k2 VPN server entry. Defined here as a forward reference;
// full fields added in Task 9.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
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
