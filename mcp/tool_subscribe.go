package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SubscribeInput is the input schema for the subscribe tool.
type SubscribeInput struct {
	PlanID       string `json:"plan_id"       description:"Plan ID to subscribe to (from list_plans)"`
	CampaignCode string `json:"campaign_code" description:"Optional campaign/coupon code"`
}

// orderRaw is the raw shape of the order response from POST /api/user/orders.
type orderRaw struct {
	OrderID              string `json:"orderId"`
	PaymentURL           string `json:"paymentUrl"`
	AmountCents          int    `json:"amount"`
	OriginalAmountCents  int    `json:"originalAmount"`
	DiscountCents        int    `json:"discount"`
	PlanName             string `json:"planName"`
}

// subscribeOutput is the shape returned to the MCP client.
type subscribeOutput struct {
	OrderID             string `json:"order_id"`
	PaymentURL          string `json:"payment_url"`
	AmountCents         int    `json:"amount_cents"`
	OriginalAmountCents int    `json:"original_amount_cents"`
	DiscountCents       int    `json:"discount_cents"`
	PlanName            string `json:"plan_name"`
}

// toolSubscribe implements the subscribe MCP tool.
func (app *App) toolSubscribe(ctx context.Context, req *mcp.CallToolRequest, in SubscribeInput) (*mcp.CallToolResult, any, error) {
	if !app.session.LoggedIn() {
		return errorResult("not logged in, please call login first"), nil, nil
	}

	body := map[string]any{
		"preview":      false,
		"plan":         in.PlanID,
		"campaignCode": in.CampaignCode,
		"forMyself":    true,
	}

	var order orderRaw
	if err := app.center.Post("/api/user/orders", body, &order); err != nil {
		return app.handleCenterError(err), nil, nil
	}

	out := subscribeOutput{
		OrderID:             order.OrderID,
		PaymentURL:          order.PaymentURL,
		AmountCents:         order.AmountCents,
		OriginalAmountCents: order.OriginalAmountCents,
		DiscountCents:       order.DiscountCents,
		PlanName:            order.PlanName,
	}
	return successResult(out), nil, nil
}
