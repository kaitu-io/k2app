package sidecar

import (
	"encoding/json"
	"strings"
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

// TestBuildNodeUpsertRequestCarriesPool: K2_NODE_POOL 每次注册都随请求体上报（wire key "pool"），
// 未配置时整个字段省略——老节点请求体逐字节不变，Center 按 '' 会员池处理。
func TestBuildNodeUpsertRequestCarriesPool(t *testing.T) {
	n := &Node{Country: "US", Name: "t", Secret: "s", IPType: "unknown", Pool: "trial"}
	b, err := json.Marshal(n.buildNodeUpsertRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"pool":"trial"`) {
		t.Fatalf("pool missing from wire body: %s", b)
	}

	n.Pool = ""
	b, err = json.Marshal(n.buildNodeUpsertRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"pool"`) {
		t.Fatalf("empty pool must be omitted: %s", b)
	}
}
