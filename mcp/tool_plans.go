package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// planRaw is the raw shape of a plan from GET /api/plans.
type planRaw struct {
	Pid         string `json:"pid"`
	Label       string `json:"label"`
	Price       int    `json:"price"`
	OriginPrice int    `json:"originPrice"`
	Month       int    `json:"month"`
	Highlight   string `json:"highlight"`
	IsActive    bool   `json:"isActive"`
}

// planOutput is the shape returned to the MCP client.
type planOutput struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	PriceCents          int    `json:"price_cents"`
	OriginalPriceCents  int    `json:"original_price_cents"`
	Months              int    `json:"months"`
	Highlight           string `json:"highlight,omitempty"`
}

// toolListPlans implements the list_plans MCP tool.
func (app *App) toolListPlans(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	var raw []planRaw
	if err := app.center.Get("/api/plans", &raw); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	var plans []planOutput
	for _, p := range raw {
		if !p.IsActive {
			continue
		}
		plans = append(plans, planOutput{
			ID:                 p.Pid,
			Name:               p.Label,
			PriceCents:         p.Price,
			OriginalPriceCents: p.OriginPrice,
			Months:             p.Month,
			Highlight:          p.Highlight,
		})
	}

	if plans == nil {
		plans = []planOutput{}
	}

	return successResult(plans), nil, nil
}
