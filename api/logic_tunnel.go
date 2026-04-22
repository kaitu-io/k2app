package center

import (
	"context"

	db "github.com/wordgate/qtoolkit/db"
)

// fetchK2V5Tunnels returns all non-deleted K2V5 tunnels with their associated
// Node preloaded. Admin callers pass includeTestTunnels=true to see IsTest=true
// rows; non-admin callers pass false.
//
// Filtering by country / protocol / capability belongs in the caller. The tunnel
// set is small (<100) so an in-memory for-loop is simpler and cheaper than
// pushing predicates into GORM joins, which have subtle aliasing traps — commit
// 3e20b8e shipped `Joins("Node")` that aliased slave_nodes as `Node` and caused
// every `/api/subs?country=XX` call to return HTTP 500.
func fetchK2V5Tunnels(ctx context.Context, includeTestTunnels bool) ([]SlaveTunnel, error) {
	q := db.Get().WithContext(ctx).
		Model(&SlaveTunnel{}).
		Preload("Node").
		Where("node_id IS NOT NULL").
		Where("protocol IN ?", tunnelProtocolsForQuery(TunnelProtocolK2V5))
	if !includeTestTunnels {
		q = q.Where(&SlaveTunnel{IsTest: BoolPtr(false)})
	}
	var tunnels []SlaveTunnel
	if err := q.Find(&tunnels).Error; err != nil {
		return nil, err
	}
	return tunnels, nil
}
