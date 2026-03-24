package main

import (
	"context"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverCacheTTL = 5 * time.Minute

// tunnelNode is the nested node object in a tunnel entry.
type tunnelNode struct {
	Name string `json:"name"`
}

// tunnelEntry is a single entry from GET /api/tunnels/k2v5.
type tunnelEntry struct {
	ID                    int        `json:"id"`
	Name                  string     `json:"name"`
	Domain                string     `json:"domain"`
	Country               string     `json:"country"`
	Region                string     `json:"region"`
	TrafficUsagePercent   float64    `json:"trafficUsagePercent"`
	BandwidthUsagePercent float64    `json:"bandwidthUsagePercent"`
	ServerURL             string     `json:"serverUrl"`
	Node                  tunnelNode `json:"node"`
}

// fetchServers returns the cached server list, refreshing from the Center API if stale.
func (app *App) fetchServers() ([]Server, error) {
	// Check cache under read lock.
	app.serversMu.RLock()
	if time.Since(app.serversCachedAt) < serverCacheTTL && len(app.servers) > 0 {
		cached := make([]Server, len(app.servers))
		copy(cached, app.servers)
		app.serversMu.RUnlock()
		return cached, nil
	}
	app.serversMu.RUnlock()

	// Fetch from API (no lock held).
	var tunnels []tunnelEntry
	if err := app.center.Get("/api/tunnels/k2v5", &tunnels); err != nil {
		return nil, err
	}

	servers := make([]Server, 0, len(tunnels))
	for _, t := range tunnels {
		name := t.Name
		if name == "" {
			name = t.Node.Name
		}

		// Extract domain from ServerURL if Domain field is empty.
		domain := t.Domain
		if domain == "" && t.ServerURL != "" {
			if u, err := url.Parse(t.ServerURL); err == nil {
				domain = u.Hostname()
			}
		}

		servers = append(servers, Server{
			ID:                    t.ID,
			Name:                  name,
			Domain:                domain,
			Country:               t.Country,
			Region:                t.Region,
			TrafficUsagePercent:   t.TrafficUsagePercent,
			BandwidthUsagePercent: t.BandwidthUsagePercent,
			ServerURL:             t.ServerURL,
		})
	}

	// Store in cache under write lock.
	app.serversMu.Lock()
	app.servers = servers
	app.serversCachedAt = time.Now()
	app.serversMu.Unlock()

	return servers, nil
}

// findServer looks up a server by ID from the cache under RLock.
// Returns nil if not found.
func (app *App) findServer(id int) *Server {
	app.serversMu.RLock()
	defer app.serversMu.RUnlock()
	for i := range app.servers {
		if app.servers[i].ID == id {
			s := app.servers[i]
			return &s
		}
	}
	return nil
}

// findServerByDomain looks up a server by domain from the cache under RLock.
// Returns nil if not found.
func (app *App) findServerByDomain(domain string) *Server {
	app.serversMu.RLock()
	defer app.serversMu.RUnlock()
	for i := range app.servers {
		if app.servers[i].Domain == domain {
			s := app.servers[i]
			return &s
		}
	}
	return nil
}

// toolListServers implements the list_servers MCP tool.
func (app *App) toolListServers(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if !app.session.LoggedIn() {
		return errorResult("not logged in, please call login first"), nil, nil
	}

	servers, err := app.fetchServers()
	if err != nil {
		return app.handleCenterError(err), nil, nil
	}

	return successResult(map[string]any{"servers": servers}), nil, nil
}
