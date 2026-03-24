package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestToolListPlans_Success(t *testing.T) {
	plans := []planRaw{
		{Pid: "p1", Label: "Monthly", Price: 999, OriginPrice: 1299, Month: 1, Highlight: "Popular", IsActive: true},
		{Pid: "p2", Label: "Annual", Price: 8999, OriginPrice: 11999, Month: 12, Highlight: "Best value", IsActive: true},
		{Pid: "p3", Label: "Legacy", Price: 500, OriginPrice: 500, Month: 1, Highlight: "", IsActive: false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plans" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		data, _ := json.Marshal(plans)
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	result, _, err := app.toolListPlans(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out []planOutput
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Only 2 active plans should be returned.
	if len(out) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(out))
	}

	// Verify first plan.
	if out[0].ID != "p1" {
		t.Errorf("expected id 'p1', got %q", out[0].ID)
	}
	if out[0].Name != "Monthly" {
		t.Errorf("expected name 'Monthly', got %q", out[0].Name)
	}
	if out[0].PriceCents != 999 {
		t.Errorf("expected price_cents=999, got %d", out[0].PriceCents)
	}
	if out[0].OriginalPriceCents != 1299 {
		t.Errorf("expected original_price_cents=1299, got %d", out[0].OriginalPriceCents)
	}
	if out[0].Months != 1 {
		t.Errorf("expected months=1, got %d", out[0].Months)
	}
	if out[0].Highlight != "Popular" {
		t.Errorf("expected highlight='Popular', got %q", out[0].Highlight)
	}

	// Verify second plan.
	if out[1].ID != "p2" {
		t.Errorf("expected id 'p2', got %q", out[1].ID)
	}
	if out[1].Months != 12 {
		t.Errorf("expected months=12, got %d", out[1].Months)
	}

	// Verify inactive plan is excluded.
	for _, p := range out {
		if p.ID == "p3" {
			t.Error("inactive plan 'p3' should not be in results")
		}
	}
}
