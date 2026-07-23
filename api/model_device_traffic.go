package center

import (
	"fmt"
	"time"
)

// cnZone is the accounting timezone for user-level traffic (spec §7):
// all "day"/"month" windows are Asia/Shanghai calendar windows.
var cnZone = time.FixedZone("CST", 8*3600)

// trafficDate truncates t to the accounting day ("2006-01-02", Asia/Shanghai).
// The ONLY place this truncation may be implemented.
func trafficDate(t time.Time) string { return t.In(cnZone).Format("2006-01-02") }

// trafficMonthRange expands "2026-07" to its first/last accounting day.
func trafficMonthRange(month string) (start, end string, err error) {
	m, perr := time.ParseInLocation("2006-01", month, cnZone)
	if perr != nil {
		return "", "", fmt.Errorf("bad month %q: %w", month, perr)
	}
	return m.Format("2006-01-02"), m.AddDate(0, 1, -1).Format("2006-01-02"), nil
}

// DeviceTrafficDaily is the per-(day, device, node) byte ledger — the fact
// table for user-level accounting. Monthly totals are SUM over the month's
// date window; there is deliberately no reset anywhere.
type DeviceTrafficDaily struct {
	ID        uint64 `gorm:"primarykey"`
	Date      string `gorm:"type:varchar(10);not null;uniqueIndex:idx_dtd_key,priority:1;index:idx_dtd_user,priority:2"`
	UDID      string `gorm:"column:udid;type:varchar(64);not null;uniqueIndex:idx_dtd_key,priority:2"`
	NodeIpv4  string `gorm:"type:varchar(15);not null;uniqueIndex:idx_dtd_key,priority:3"`
	UserID    uint   `gorm:"not null;default:0;index:idx_dtd_user,priority:1"`
	RxBytes   int64  `gorm:"not null;default:0"`
	TxBytes   int64  `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeviceTrafficCursor is the per-node idempotency cursor for device-traffic
// batches: a report with the same boot_id and batch_seq <= BatchSeq has
// already been ingested and must be skipped (ack-lost resend).
type DeviceTrafficCursor struct {
	ID        uint64 `gorm:"primarykey"`
	Ipv4      string `gorm:"type:varchar(15);uniqueIndex;not null"`
	BootID    string `gorm:"type:varchar(32);not null"`
	BatchSeq  int64  `gorm:"not null;default:0"`
	UpdatedAt time.Time
}

// TrafficAbuseAlert dedupes the monthly over-threshold Slack alert:
// one row = one (month, user) already alerted.
type TrafficAbuseAlert struct {
	ID        uint64 `gorm:"primarykey"`
	Month     string `gorm:"type:varchar(7);not null;uniqueIndex:idx_taa_key,priority:1"`
	UserID    uint   `gorm:"not null;uniqueIndex:idx_taa_key,priority:2"`
	Bytes     int64  `gorm:"not null;default:0"`
	CreatedAt time.Time
}
