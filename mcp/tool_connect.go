package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectInput is the input schema for the connect tool.
type ConnectInput struct {
	ServerID int `json:"server_id" description:"ID of the server to connect to"`
}

// toolConnect implements the connect MCP tool.
func (app *App) toolConnect(ctx context.Context, req *mcp.CallToolRequest, in ConnectInput) (*mcp.CallToolResult, any, error) {
	if !app.session.LoggedIn() {
		return errorResult("not logged in, please call login first"), nil, nil
	}

	// Populate/use server cache.
	if _, err := app.fetchServers(); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	// Resolve server by ID.
	server := app.findServer(in.ServerID)
	if server == nil {
		return errorResult("server not found: unknown server_id"), nil, nil
	}

	// Check daemon is reachable.
	if err := app.daemon.Ping(); err != nil {
		return errorResult("k2 daemon is not running — start it with: k2 ctl up"), nil, nil
	}

	// Issue connect command.
	if err := app.daemon.Up(server.ServerURL); err != nil {
		return errorResult("failed to connect: " + err.Error()), nil, nil
	}

	return successResult(map[string]string{
		"state":  "connecting",
		"server": server.Name,
	}), nil, nil
}
