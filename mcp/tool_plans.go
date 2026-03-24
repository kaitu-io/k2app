package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// planRaw is the raw shape of a plan from GET /api/plans.
type planRaw struct {
	Pid         string `json:"pid"`
	Label       string `json:"label"`
	Price       int    `json:"price"`
	OriginPrice int    `json:"originPrice"`
	Month       int    `json:"month"`
	Highlight   bool   `json:"highlight"`
	IsActive    bool   `json:"isActive"`
}

// planOutput is the shape returned to the MCP client.
type planOutput struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Price         string `json:"price"`
	OriginalPrice string `json:"original_price,omitempty"`
	Months        int    `json:"months"`
	Highlight     bool   `json:"highlight"`
}

// toolListPlans implements the list_plans MCP tool.
func (app *App) toolListPlans(ctx context.Context, req *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	var resp struct {
		Items []planRaw `json:"items"`
	}
	if err := app.center.Get("/api/plans", &resp); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	var plans []planOutput
	for _, p := range resp.Items {
		if !p.IsActive {
			continue
		}
		out := planOutput{
			ID:        p.Pid,
			Name:      p.Label,
			Price:     fmt.Sprintf("$%.2f", float64(p.Price)/100),
			Months:    p.Month,
			Highlight: p.Highlight,
		}
		if p.OriginPrice > p.Price {
			out.OriginalPrice = fmt.Sprintf("$%.2f", float64(p.OriginPrice)/100)
		}
		plans = append(plans, out)
	}

	if plans == nil {
		plans = []planOutput{}
	}

	return successResult(map[string]any{"plans": plans}), nil, nil
}
