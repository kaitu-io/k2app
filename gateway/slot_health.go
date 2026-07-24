//go:build linux

package gateway

import (
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/probe"
	"github.com/kaitu-io/k2/wire"
)

// SlotStatus is one row of the enterprise self-description in
// /api/core status. Its presence (non-empty slots array) is THE signal
// the app uses to render the enterprise form — consumer mode never
// emits it.
type SlotStatus struct {
	Slot      int        `json:"slot"`
	SSID      string     `json:"ssid"`
	Country   string     `json:"country"`
	Index     int        `json:"index"`
	State     string     `json:"state"` // running | failClosed | disabled
	DownSince *time.Time `json:"downSince,omitempty"`
}

// slotURLSource feeds the probe service the bound tunnel URLs.
type slotURLSource struct{ g *Gateway }

func (s slotURLSource) CandidateURLs() []string {
	s.g.mu.Lock()
	defer s.g.mu.Unlock()
	urls := make([]string, 0, len(s.g.slotTunnelURLs))
	for _, u := range s.g.slotTunnelURLs {
		urls = append(urls, u)
	}
	return urls
}

func (s slotURLSource) DirectDialer() wire.DirectDialer { return &gatewayDirectDialer{} }

// slotStatuses builds the 8-row self-description. nil in consumer mode.
func (g *Gateway) slotStatuses() []SlotStatus {
	// loadSlotLocal 读磁盘 JSON，按其文档在锁外调用（Rule #2: no I/O under lock）。
	local := loadSlotLocal()
	// Rule #5: 显式 Unlock() 而非 defer——本函数体 >15 行，defer 会让整个循环体隐式持锁。
	g.mu.Lock()
	if len(g.slotBindings) == 0 {
		g.mu.Unlock()
		return nil
	}
	bound := map[int]config.SlotBinding{}
	for _, b := range g.slotBindings {
		bound[b.Slot] = b
	}
	now := time.Now()
	out := make([]SlotStatus, 0, maxSlots)
	for slot := 1; slot <= maxSlots; slot++ {
		b, ok := bound[slot]
		if !ok {
			out = append(out, SlotStatus{Slot: slot, State: "disabled"})
			continue
		}
		st := SlotStatus{Slot: slot, SSID: slotSSID(b), Country: b.Country, Index: b.Index, State: "running"}
		if local.TakenOver[slot] {
			st.SSID = "" // customer-set; router-local UCI owns it, we don't read it back
		}
		if url := g.slotTunnelURLs[slot]; url != "" && g.slotRegistry != nil {
			if score, ok := g.slotRegistry.Score(url); ok && score == 0 {
				st.State = "failClosed"
				if _, dn := g.slotDownSince[slot]; !dn {
					g.slotDownSince[slot] = now
				}
				t := g.slotDownSince[slot]
				st.DownSince = &t
			} else {
				delete(g.slotDownSince, slot)
			}
		}
		out = append(out, st)
	}
	g.mu.Unlock()
	return out
}

// startSlotHealth spins the probe loop for multi-slot mode. 60s cadence:
// fail-closed red light latency beats probe thrift here.
func (g *Gateway) startSlotHealth() {
	g.mu.Lock()
	g.slotRegistry = probe.NewRegistry(15 * time.Minute)
	g.slotDownSince = map[int]time.Time{}
	g.slotProbeSvc = probe.NewService(probe.Config{
		Registry: g.slotRegistry,
		Source:   slotURLSource{g: g},
		Cadence:  time.Minute,
	})
	svc, ctx := g.slotProbeSvc, g.engineCtx
	g.mu.Unlock()
	svc.Start(ctx)
}
