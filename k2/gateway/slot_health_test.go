//go:build linux

package gateway

import (
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/wire"
)

// healthyProbe/deadProbe build realistic wire.ProbeStats fixtures.
// wire.ProbeStats.Score() is a computed method (Reachable/EchoSupported/
// AvgRttMs/LossRate/JitterMs), not a settable field.
func healthyProbe() wire.ProbeStats {
	return wire.ProbeStats{Reachable: true, EchoSupported: true, AvgRttMs: 30, LossRate: 0, JitterMs: 10, MeasuredAt: time.Now()}
}

func deadProbe() wire.ProbeStats {
	return wire.ProbeStats{Reachable: false, MeasuredAt: time.Now()}
}

func TestSlotStatuses(t *testing.T) {
	g := newTestGateway(t)
	reg := probe.NewRegistry(15 * time.Minute)
	g.mu.Lock()
	g.slotBindings = []config.SlotBinding{
		{Slot: 1, Country: "ae", Index: 1, TunnelIndex: 0},
		{Slot: 2, Country: "gn", Index: 1, TunnelIndex: 1},
	}
	g.slotTunnelURLs = map[int]string{1: "k2v5://u:p@1.1.1.1:443", 2: "k2v5://u:p@2.2.2.2:443"}
	g.slotRegistry = reg
	g.slotDownSince = map[int]time.Time{}
	g.mu.Unlock()

	// slot1 健康(score>0);slot2 连续两次 0 分 → failClosed
	reg.Record("k2v5://u:p@1.1.1.1:443", healthyProbe())
	reg.Record("k2v5://u:p@2.2.2.2:443", deadProbe())
	reg.Record("k2v5://u:p@2.2.2.2:443", deadProbe())

	slots := g.slotStatuses()
	if len(slots) != 8 {
		t.Fatalf("want 8 slots always, got %d", len(slots))
	}
	if slots[0].State != "running" || slots[0].SSID != "overleap-ae-1" || slots[0].Country != "ae" {
		t.Fatalf("slot1: %+v", slots[0])
	}
	if slots[1].State != "failClosed" || slots[1].DownSince == nil {
		t.Fatalf("slot2 must be failClosed with DownSince: %+v", slots[1])
	}
	if slots[2].State != "disabled" {
		t.Fatalf("unbound slot3 must be disabled: %+v", slots[2])
	}
	// 恢复:score>0 → running,DownSince 清空
	reg.Record("k2v5://u:p@2.2.2.2:443", healthyProbe())
	slots = g.slotStatuses()
	if slots[1].State != "running" || slots[1].DownSince != nil {
		t.Fatalf("slot2 must recover: %+v", slots[1])
	}
}

func TestSlotStatuses_ConsumerModeEmpty(t *testing.T) {
	g := newTestGateway(t)
	if got := g.slotStatuses(); got != nil {
		t.Fatalf("consumer mode must return nil (field omitted), got %+v", got)
	}
}
