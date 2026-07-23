package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeviceTrafficRequest — per-device byte DELTAS from a node's
// deviceTrafficReporter. JSON tags MUST match k2's server.DeviceTrafficRequest
// exactly (k2/server/device_traffic_reporter.go). A wrong tag silently breaks
// accounting.
type DeviceTrafficRequest struct {
	BootID   string              `json:"boot_id"`
	BatchSeq int64               `json:"batch_seq"`
	Ts       int64               `json:"ts"`
	Devices  []DeviceTrafficItem `json:"devices"`
}

type DeviceTrafficItem struct {
	UDID string `json:"udid"`
	Rx   int64  `json:"rx"`
	Tx   int64  `json:"tx"`
}

// api_slave_device_traffic records POST /slave/device-traffic.
// Idempotency: same boot_id with batch_seq <= cursor → already ingested
// (ack-lost resend), skip but still ack success.
func api_slave_device_traffic(c *gin.Context) {
	node := ReqSlaveNode(c)
	if node == nil {
		Error(c, ErrorNotLogin, "node context required")
		return
	}
	if node.Ipv4 == "" {
		// Same fail-safe stance as /slave/usage: empty key must not write.
		log.Warnf(c, "[DEVTRAFFIC] node=%d has empty ipv4; skipping", node.ID)
		SuccessEmpty(c)
		return
	}
	var req DeviceTrafficRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "bad device traffic payload")
		return
	}
	if err := ingestDeviceTraffic(c, node.Ipv4, &req); err != nil {
		log.Errorf(c, "[DEVTRAFFIC] ingest ip=%s: %v", node.Ipv4, err)
		Error(c, ErrorSystemError, "ingest failed")
		return
	}
	SuccessEmpty(c)
}

// ingestDeviceTraffic performs cursor check, user resolution, and daily
// upsert. gin context is only used for logging and may be nil in tests.
func ingestDeviceTraffic(c *gin.Context, ipv4 string, req *DeviceTrafficRequest) error {
	// 1) idempotency cursor
	var cur DeviceTrafficCursor
	err := db.Get().Where("ipv4 = ?", ipv4).First(&cur).Error
	if err == nil && cur.BootID == req.BootID && req.BatchSeq <= cur.BatchSeq {
		return nil // duplicate (ack-lost resend) — already ingested
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if len(req.Devices) > 0 {
		// 2) resolve udid → user_id in one IN query
		udids := make([]string, 0, len(req.Devices))
		for _, d := range req.Devices {
			if d.UDID != "" {
				udids = append(udids, d.UDID)
			}
		}
		userByUDID := map[string]uint{}
		if len(udids) > 0 {
			var devs []Device
			if derr := db.Get().Where("udid IN ?", udids).Find(&devs).Error; derr != nil {
				return derr
			}
			for _, d := range devs {
				userByUDID[d.UDID] = uint(d.UserID)
			}
		}

		// 3) daily upsert (accumulate)
		date := trafficDate(time.Now())
		rows := make([]DeviceTrafficDaily, 0, len(req.Devices))
		for _, d := range req.Devices {
			rows = append(rows, DeviceTrafficDaily{
				Date: date, UDID: d.UDID, NodeIpv4: ipv4,
				UserID: userByUDID[d.UDID], RxBytes: d.Rx, TxBytes: d.Tx,
			})
		}
		if uerr := db.Get().Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "date"}, {Name: "udid"}, {Name: "node_ipv4"}},
			DoUpdates: clause.Assignments(map[string]any{
				"rx_bytes": gorm.Expr("rx_bytes + VALUES(rx_bytes)"),
				"tx_bytes": gorm.Expr("tx_bytes + VALUES(tx_bytes)"),
				"user_id":  gorm.Expr("VALUES(user_id)"),
			}),
		}).Create(&rows).Error; uerr != nil {
			return uerr
		}
	}

	// 4) advance cursor (upsert by ipv4)
	return db.Get().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ipv4"}},
		DoUpdates: clause.Assignments(map[string]any{
			"boot_id":   req.BootID,
			"batch_seq": req.BatchSeq,
		}),
	}).Create(&DeviceTrafficCursor{Ipv4: ipv4, BootID: req.BootID, BatchSeq: req.BatchSeq}).Error
}
