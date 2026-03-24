package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errorResult returns a CallToolResult with IsError=true and a JSON error message.
func errorResult(msg string) *mcp.CallToolResult {
	data, _ := json.Marshal(map[string]string{"error": msg})
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

// successResult returns a CallToolResult with JSON-encoded v as text content.
func successResult(v any) *mcp.CallToolResult {
	data, _ := json.Marshal(v)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

// LoginInput is the input schema for the login tool.
type LoginInput struct {
	Email    string `json:"email"    description:"User email address"`
	Password string `json:"password" description:"User password"`
}

// loginResponse is the data field returned by POST /api/auth/login/password.
type loginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// toolLogin implements the login MCP tool.
func (app *App) toolLogin(ctx context.Context, req *mcp.CallToolRequest, in LoginInput) (*mcp.CallToolResult, any, error) {
	body := map[string]any{
		"email":    in.Email,
		"password": in.Password,
		"udid":     app.session.UDID(),
		"remark":   "k2-mcp",
		"platform": "mcp",
	}

	var resp loginResponse
	if err := app.center.Post("/api/auth/login/password", body, &resp); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	app.session.SetTokens(resp.AccessToken, resp.RefreshToken, in.Email, time.Now())
	app.center.SetToken(resp.AccessToken)

	app.session.Save() //nolint:errcheck // non-fatal: tokens are in-memory

	return successResult(map[string]string{"email": in.Email}), nil, nil
}
