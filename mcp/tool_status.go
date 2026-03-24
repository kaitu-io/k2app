package main

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveServerName parses serverURL, extracts its hostname, and looks it up
// in the server cache. Falls back to the raw domain if no match is found.
func (app *App) resolveServerName(serverURL string) string {
	if serverURL == "" {
		return ""
	}
	u, err := url.Parse(serverURL)
	if err != nil {
		return serverURL
	}
	domain := u.Hostname()
	if s := app.findServerByDomain(domain); s != nil {
		return s.Name
	}
	return domain
}

// toolStatus implements the status MCP tool.
func (app *App) toolStatus(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	status, err := app.daemon.Status()
	if err != nil {
		// Daemon unreachable — treat as disconnected.
		return successResult(map[string]string{"state": "disconnected"}), nil, nil
	}

	out := map[string]any{
		"state": status.State,
	}

	switch status.State {
	case "connected":
		out["uptime_seconds"] = status.UptimeSeconds
		if status.Config != nil {
			out["server"] = app.resolveServerName(status.Config.Server)
		}
	case "error":
		if status.Error != nil {
			out["error"] = status.Error.Message
			out["error_code"] = status.Error.Code
		}
	}

	return successResult(out), nil, nil
}
