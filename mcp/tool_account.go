package main

import (
	"context"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleCenterError maps a CenterError (or generic error) to an errorResult.
func (app *App) handleCenterError(err error) *mcp.CallToolResult {
	var ce *CenterError
	if errors.As(err, &ce) {
		switch ce.Code {
		case 401:
			return errorResult("not logged in, please call login first")
		case 402:
			return errorResult("subscription expired")
		default:
			return errorResult(ce.Message)
		}
	}
	return errorResult(err.Error())
}

// userResponse is the raw shape of GET /api/user data field.
type userResponse struct {
	LoginIdentifies []struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"loginIdentifies"`
	ExpiredAt   int64      `json:"expiredAt"`
	DeviceCount int        `json:"deviceCount"`
	InviteCode  *struct {
		Code string `json:"code"`
	} `json:"inviteCode"`
}

// accountInfoOutput is the shape returned to the MCP client.
type accountInfoOutput struct {
	Email          string `json:"email"`
	PlanExpiresAt  string `json:"plan_expires_at,omitempty"`
	IsActive       bool   `json:"is_active"`
	DeviceCount    int    `json:"device_count"`
	DeviceLimit    int    `json:"device_limit"`
	InviteCode     string `json:"invite_code,omitempty"`
}

// toolAccountInfo implements the account_info MCP tool.
func (app *App) toolAccountInfo(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	if !app.session.LoggedIn() {
		return errorResult("not logged in, please call login first"), nil, nil
	}

	var user userResponse
	if err := app.center.Get("/api/user", &user); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	// Extract email from loginIdentifies.
	email := ""
	for _, ident := range user.LoginIdentifies {
		if ident.Type == "email" {
			email = ident.Value
			break
		}
	}

	planExpiresAt := ""
	isActive := false
	if user.ExpiredAt > 0 {
		planExpiresAt = time.Unix(user.ExpiredAt, 0).UTC().Format(time.RFC3339)
		isActive = user.ExpiredAt > time.Now().Unix()
	}

	inviteCode := ""
	if user.InviteCode != nil {
		inviteCode = user.InviteCode.Code
	}

	out := accountInfoOutput{
		Email:         email,
		PlanExpiresAt: planExpiresAt,
		IsActive:      isActive,
		DeviceCount:   user.DeviceCount,
		DeviceLimit:   5,
		InviteCode:    inviteCode,
	}
	return successResult(out), nil, nil
}
