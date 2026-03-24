package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolDisconnect implements the disconnect MCP tool.
func (app *App) toolDisconnect(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if err := app.daemon.Down(); err != nil {
		// If daemon is unreachable, treat as already disconnected.
		return successResult(map[string]string{"status": "disconnected"}), nil, nil
	}
	return successResult(map[string]string{"status": "disconnected"}), nil, nil
}
