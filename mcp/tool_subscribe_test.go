package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestToolSubscribe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/orders" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["plan"] != "p1" {
			t.Errorf("expected plan 'p1', got %v", body["plan"])
		}
		if body["preview"] != false {
			t.Errorf("expected preview=false, got %v", body["preview"])
		}
		if body["forMyself"] != true {
			t.Errorf("expected forMyself=true, got %v", body["forMyself"])
		}

		data, _ := json.Marshal(orderRaw{
			PayURL: "https://pay.example.com/order/xyz",
			Order: orderDataRaw{
				UUID:                 "order-xyz-123",
				Title:                "Monthly",
				OriginAmount:         1299,
				CampaignReduceAmount: 300,
				PayAmount:            999,
			},
		})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	app.session.SetTokens("access-token", "refresh-token", "test@example.com", time.Now())
	app.center.SetToken("access-token")

	result, _, err := app.toolSubscribe(context.Background(), nil, SubscribeInput{
		PlanID:       "p1",
		CampaignCode: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out subscribeOutput
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if out.OrderID != "order-xyz-123" {
		t.Errorf("expected order_id 'order-xyz-123', got %q", out.OrderID)
	}
	if out.PaymentURL != "https://pay.example.com/order/xyz" {
		t.Errorf("expected payment_url, got %q", out.PaymentURL)
	}
	if out.AmountUSD != "$9.99" {
		t.Errorf("expected amount_usd '$9.99', got %q", out.AmountUSD)
	}
	if out.DiscountUSD != "$3.00" {
		t.Errorf("expected discount_usd '$3.00', got %q", out.DiscountUSD)
	}
	if out.Plan != "Monthly" {
		t.Errorf("expected plan 'Monthly', got %q", out.Plan)
	}
}

func TestToolSubscribe_NotLoggedIn(t *testing.T) {
	app := newTestApp(t, "http://127.0.0.1:0")

	result, _, err := app.toolSubscribe(context.Background(), nil, SubscribeInput{
		PlanID: "p1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true when not logged in")
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["error"] == "" {
		t.Error("expected non-empty error message")
	}
}
