package gateway

import (
	"testing"

	"github.com/kaitu-io/k2/config"
)

func slotFixture() ([]config.SlotBinding, []config.Tunnel) {
	bindings := []config.SlotBinding{
		{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
		{Slot: 5, Country: "gn", Index: 1, TunnelIndex: 1},
	}
	tunnels := []config.Tunnel{
		{URL: "k2v5://u:p@1.1.1.1:443"},
		{URL: "k2v5://u:p@2.2.2.2:443"},
	}
	return bindings, tunnels
}

func TestBuildSlotRoutes(t *testing.T) {
	bindings, tunnels := slotFixture()
	routes, err := buildSlotRoutes(bindings, tunnels)
	if err != nil {
		t.Fatal(err)
	}
	// 2 slot 路由 + 1 reject catch-all
	if len(routes) != 3 {
		t.Fatalf("want 3 routes, got %d", len(routes))
	}
	if routes[0].Via != "k2v5://u:p@1.1.1.1:443" || routes[0].Match.SrcIPCIDR[0] != "10.81.1.0/24" {
		t.Fatalf("slot1 route wrong: %+v", routes[0])
	}
	if routes[1].Match.SrcIPCIDR[0] != "10.81.5.0/24" {
		t.Fatalf("slot5 subnet wrong: %+v", routes[1])
	}
	last := routes[len(routes)-1]
	if last.Via != "reject" || !last.Match.All {
		t.Fatalf("catch-all must be reject/all (fail-closed), got %+v", last)
	}
}

func TestBuildSlotRoutes_Validation(t *testing.T) {
	tunnels := []config.Tunnel{{URL: "k2v5://u:p@1.1.1.1:443"}}
	cases := []config.SlotBinding{
		{Slot: 0, Country: "ae", Index: 1, TunnelIndex: 0},  // slot 越界
		{Slot: 9, Country: "ae", Index: 1, TunnelIndex: 0},  // slot 越界
		{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 5},  // tunnel_index 越界
		{Slot: 1, Country: "AE!", Index: 1, TunnelIndex: 0}, // country 非法
	}
	for i, b := range cases {
		routes, err := buildSlotRoutes([]config.SlotBinding{b}, tunnels)
		if err != nil {
			continue // 整体报错可接受
		}
		// 非法条目被跳过 → 只剩 reject catch-all
		if len(routes) != 1 {
			t.Fatalf("case %d: invalid binding must be dropped, got %+v", i, routes)
		}
	}
	// 同 slot 重复:后者被丢弃
	dup := []config.SlotBinding{
		{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
		{Slot: 1, Country: "gn", Index: 1, TunnelIndex: 0},
	}
	routes, err := buildSlotRoutes(dup, tunnels)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 { // 1 slot + reject
		t.Fatalf("duplicate slot must be dropped, got %+v", routes)
	}
}

func TestSlotSSID(t *testing.T) {
	if got := slotSSID(config.SlotBinding{Country: "ae", Index: 1}); got != "overleap-ae-1" {
		t.Fatalf("ssid = %q", got)
	}
}

func TestSlotBindingsEqual(t *testing.T) {
	a := []config.SlotBinding{{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0}, {Slot: 2, Country: "gn", Index: 1, TunnelIndex: 1}}
	b := []config.SlotBinding{{Slot: 2, Country: "gn", Index: 1, TunnelIndex: 1}, {Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0}}
	if !slotBindingsEqual(a, b) {
		t.Fatal("order must not matter")
	}
	c := []config.SlotBinding{{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0}}
	if slotBindingsEqual(a, c) {
		t.Fatal("length diff must differ")
	}
	d := []config.SlotBinding{{Slot: 1, Country: "ae", Index: 2, TunnelIndex: 0}, {Slot: 2, Country: "gn", Index: 1, TunnelIndex: 1}}
	if slotBindingsEqual(a, d) {
		t.Fatal("field diff must differ")
	}
	if !slotBindingsEqual(nil, nil) || slotBindingsEqual(nil, c) {
		t.Fatal("nil semantics")
	}
}
