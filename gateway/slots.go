package gateway

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

const (
	maxSlots        = 8
	slotDNSUpstream = "1.1.1.1:53"
)

var countryRe = regexp.MustCompile(`^[a-z]{2}$`)

// slotSubnet returns the factory-baked bridge subnet for a slot (br-lineN).
// Third octet == slot number: the whole slot attribution mechanism.
func slotSubnet(slot int) string {
	return fmt.Sprintf("10.81.%d.0/24", slot)
}

// slotSSID composes the default SSID for a binding: overleap-<cc>-<index>.
func slotSSID(b config.SlotBinding) string {
	return fmt.Sprintf("overleap-%s-%d", b.Country, b.Index)
}

// validSlotBinding filters manifest entries defensively. Invalid entries
// are dropped (logged), never fatal — a partially-valid manifest still
// serves its valid slots; dropped slots stay fail-closed via catch-all.
func validSlotBinding(b config.SlotBinding, tunnelCount int, seen map[int]bool) bool {
	switch {
	case b.Slot < 1 || b.Slot > maxSlots:
		slog.Warn("DIAG: slot-binding-invalid", "reason", "slot-range", "slot", b.Slot)
	case b.TunnelIndex < 0 || b.TunnelIndex >= tunnelCount:
		slog.Warn("DIAG: slot-binding-invalid", "reason", "tunnel-index", "slot", b.Slot, "idx", b.TunnelIndex)
	case !countryRe.MatchString(b.Country):
		slog.Warn("DIAG: slot-binding-invalid", "reason", "country", "slot", b.Slot, "country", b.Country)
	case seen[b.Slot]:
		slog.Warn("DIAG: slot-binding-invalid", "reason", "duplicate-slot", "slot", b.Slot)
	default:
		return true
	}
	return false
}

// buildSlotRoutes converts the manifest into deterministic per-slot routes.
// Every slot subnet pins to exactly one outbound; everything else —
// unbound slots, unknown sources — hits the reject catch-all. No direct
// fallback anywhere: fail-closed is the contract.
func buildSlotRoutes(bindings []config.SlotBinding, tunnels []config.Tunnel) ([]config.RouteConfig, error) {
	seen := map[int]bool{}
	var routes []config.RouteConfig
	for _, b := range bindings {
		if !validSlotBinding(b, len(tunnels), seen) {
			continue
		}
		seen[b.Slot] = true
		routes = append(routes, config.RouteConfig{
			Via:   tunnels[b.TunnelIndex].URL,
			Match: rule.MatchConfig{SrcIPCIDR: []string{slotSubnet(b.Slot)}},
		})
	}
	routes = append(routes, config.RouteConfig{Via: "reject", Match: rule.MatchConfig{All: true}})
	return routes, nil
}

// slotBindingsEqual compares manifests order-insensitively.
func slotBindingsEqual(a, b []config.SlotBinding) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[int]config.SlotBinding{}
	for _, x := range a {
		am[x.Slot] = x
	}
	for _, y := range b {
		if am[y.Slot] != y {
			return false
		}
	}
	return true
}
