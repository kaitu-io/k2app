package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SubscribeInput is the input schema for the subscribe tool.
type SubscribeInput struct {
	PlanID       string `json:"plan_id"       description:"Plan ID to subscribe to (from list_plans)"`
	CampaignCode string `json:"campaign_code" description:"Optional campaign/coupon code"`
}

// orderRaw is the raw shape from POST /api/user/orders.
type orderRaw struct {
	PayURL string        `json:"payUrl"`
	Order  orderDataRaw  `json:"order"`
}

type orderDataRaw struct {
	ID                   string `json:"id"`
	UUID                 string `json:"uuid"`
	Title                string `json:"title"`
	OriginAmount         int    `json:"originAmount"`
	CampaignReduceAmount int    `json:"campaignReduceAmount"`
	PayAmount            int    `json:"payAmount"`
}

// subscribeOutput is the shape returned to the MCP client.
type subscribeOutput struct {
	OrderID       string `json:"order_id"`
	PaymentURL    string `json:"payment_url"`
	Plan          string `json:"plan"`
	AmountUSD     string `json:"amount_usd"`
	DiscountUSD   string `json:"discount_usd,omitempty"`
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

	orderID := order.Order.UUID
	if orderID == "" {
		orderID = order.Order.ID
	}

	out := subscribeOutput{
		OrderID:    orderID,
		PaymentURL: order.PayURL,
		Plan:       order.Order.Title,
		AmountUSD:  fmt.Sprintf("$%.2f", float64(order.Order.PayAmount)/100),
	}
	if order.Order.CampaignReduceAmount > 0 {
		out.DiscountUSD = fmt.Sprintf("$%.2f", float64(order.Order.CampaignReduceAmount)/100)
	}
	return successResult(out), nil, nil
}
