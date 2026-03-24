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

// SendCodeInput is the input schema for the send_code tool.
type SendCodeInput struct {
	Email string `json:"email" description:"User email address"`
}

// sendCodeResponse is the data field returned by POST /api/auth/code.
type sendCodeResponse struct {
	UserExists       bool `json:"userExists"`
	IsActivated      bool `json:"isActivated"`
	IsFirstOrderDone bool `json:"isFirstOrderDone"`
}

// toolSendCode implements the send_code MCP tool.
func (app *App) toolSendCode(ctx context.Context, req *mcp.CallToolRequest, in SendCodeInput) (*mcp.CallToolResult, any, error) {
	body := map[string]string{
		"email": in.Email,
	}

	var resp sendCodeResponse
	if err := app.center.Post("/api/auth/code", body, &resp); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	return successResult(map[string]any{
		"email":   in.Email,
		"message": "verification code sent",
	}), nil, nil
}

// LoginInput is the input schema for the login tool.
type LoginInput struct {
	Email            string `json:"email"             description:"User email address"`
	VerificationCode string `json:"verification_code" description:"Verification code received via email"`
}

// loginResponse is the data field returned by POST /api/auth/login.
type loginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	IssuedAt     int64  `json:"issuedAt"`
}

// toolLogin implements the login MCP tool.
func (app *App) toolLogin(ctx context.Context, req *mcp.CallToolRequest, in LoginInput) (*mcp.CallToolResult, any, error) {
	body := map[string]any{
		"email":            in.Email,
		"verificationCode": in.VerificationCode,
		"udid":             app.session.UDID(),
		"remark":           "k2-mcp",
	}

	var resp loginResponse
	if err := app.center.Post("/api/auth/login", body, &resp); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	issuedAt := time.Unix(resp.IssuedAt, 0)
	app.session.SetTokens(resp.AccessToken, resp.RefreshToken, in.Email, issuedAt)
	app.center.SetToken(resp.AccessToken)

	app.session.Save() //nolint:errcheck // non-fatal: tokens are in-memory

	return successResult(map[string]string{"email": in.Email}), nil, nil
}
