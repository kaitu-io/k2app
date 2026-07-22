package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SubsSlotBinding is the wire form of one router-slot binding in the
// /api/subs response. Cross-repo contract with k2 config.SlotBinding
// (snake_case, tunnel_index indexes the same response's tunnels array).
type SubsSlotBinding struct {
	Slot        int    `json:"slot"`
	Country     string `json:"country"`
	Index       int    `json:"index"`
	TunnelIndex int    `json:"tunnel_index"`
}

// resolveSlotBindings joins the operator-maintained binding matrix into
// the manifest for one gateway device. Returns nil for non-enterprise
// gateways (field omitted → k2r stays in consumer mode).
//
// Deliberately health-blind: the manifest reflects the Binding table and
// nothing else. Quota cutoff, node death, load — none of it removes a
// slot here; removal is a rebind, and rebinds are human-only.
//
// tunnels must be indexed identically to the same /api/subs response's
// `tunnels` array — TunnelIndex is a positional reference into it.
func resolveSlotBindings(ctx context.Context, gatewayDeviceID uint64, tunnels []SlaveTunnel) []SubsSlotBinding {
	var rows []EnterpriseRouterBinding
	if err := db.Get().WithContext(ctx).
		Preload("Line").
		Where("gateway_device_id = ?", gatewayDeviceID).
		Order("slot").
		Find(&rows).Error; err != nil {
		log.Errorf(ctx, "enterprise: load bindings failed dev=%d: %v", gatewayDeviceID, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	nodeIdx := map[uint64]int{}
	for i, t := range tunnels {
		if _, ok := nodeIdx[t.NodeID]; !ok {
			nodeIdx[t.NodeID] = i
		}
	}
	var out []SubsSlotBinding
	for _, b := range rows {
		if b.Line == nil || b.Line.Status != "active" {
			continue
		}
		idx, ok := nodeIdx[b.Line.NodeID]
		if !ok {
			// Line's node not in the serviceable tunnel set (entitlement
			// lapsed / node deleted). Slot omitted → k2r disables its SSID.
			log.Warnf(ctx, "enterprise: slot %d line %d node %d not serviceable, omitting", b.Slot, b.LineID, b.Line.NodeID)
			continue
		}
		out = append(out, SubsSlotBinding{Slot: b.Slot, Country: b.Line.CountryCode, Index: b.Line.LineNo, TunnelIndex: idx})
	}
	return out
}
