package sidecar

import (
	"testing"
)

// TestBuildNodeUpsertRequestCarriesIPType verifies that buildNodeUpsertRequest
// copies the Node's IPType into the upsert request body, so Center receives
// the correct ip_type value on registration.
func TestBuildNodeUpsertRequestCarriesIPType(t *testing.T) {
	n := &Node{Country: "US", Name: "t", Secret: "s", IPType: "residential"}
	req := n.buildNodeUpsertRequest(nil)
	if req.IPType != "residential" {
		t.Fatalf("IPType=%q want residential", req.IPType)
	}
	// Zero-value IPType passes through unchanged; the "unknown" default is
	// guaranteed by main.go's NewSidecar fallback (not by the request builder).
}
