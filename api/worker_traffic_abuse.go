package center

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// TaskTypeTrafficAbuseCheck is the hourly Asynq cron task that scans the
// current CST-calendar-month per-user traffic total, Slack-alerts once per
// (month, user) via TrafficAbuseAlert dedup, and sweeps DeviceTrafficDaily
// rows past the retention window.
const TaskTypeTrafficAbuseCheck = "traffic:abuse_check"

// trafficRetentionDays is how long DeviceTrafficDaily rows are kept before
// cleanupTrafficRetention deletes them.
const trafficRetentionDays = 180

// trafficAbuseThresholdBytes reads config `traffic.abuse_monthly_gb`
// (default 500 GB when unset/non-positive) as bytes.
func trafficAbuseThresholdBytes() int64 {
	gb := viper.GetInt64("traffic.abuse_monthly_gb")
	if gb <= 0 {
		gb = 500
	}
	return gb << 30
}

// trafficAbuser is one over-threshold user for the scanned month.
type trafficAbuser struct {
	UserID uint
	Bytes  int64
}

// findTrafficAbusers returns users whose current-month total (rx+tx, summed
// across all devices/nodes) exceeds thresholdBytes. user_id=0 (unattributed
// bucket) is excluded — nothing actionable on it.
func findTrafficAbusers(month string, thresholdBytes int64) ([]trafficAbuser, error) {
	start, end, err := trafficMonthRange(month)
	if err != nil {
		return nil, err
	}
	var rows []trafficAbuser
	err = db.Get().Model(&DeviceTrafficDaily{}).
		Select("user_id, SUM(rx_bytes+tx_bytes) AS bytes").
		Where("user_id <> 0 AND date BETWEEN ? AND ?", start, end).
		Group("user_id").
		Having("bytes > ?", thresholdBytes).
		Scan(&rows).Error
	return rows, err
}

// recordTrafficAbuseAlert inserts the (month, user) dedup row backed by the
// TrafficAbuseAlert unique index. Returns true only when this call created
// the row (= caller should notify); a pre-existing row (already alerted this
// month) returns false without error.
func recordTrafficAbuseAlert(month string, userID uint, bytes int64) (bool, error) {
	res := db.Get().Where(TrafficAbuseAlert{Month: month, UserID: userID}).
		Attrs(TrafficAbuseAlert{Bytes: bytes}).
		FirstOrCreate(&TrafficAbuseAlert{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// cleanupTrafficRetention deletes DeviceTrafficDaily rows older than the
// retention window (accounting-day string comparison — ISO "2006-01-02" is
// lexicographically ordered, so this is safe).
func cleanupTrafficRetention(days int) error {
	cutoff := time.Now().In(cnZone).AddDate(0, 0, -days).Format("2006-01-02")
	return db.Get().Where("date < ?", cutoff).Delete(&DeviceTrafficDaily{}).Error
}

// handleTrafficAbuseCheck is the Asynq cron handler for TaskTypeTrafficAbuseCheck.
func handleTrafficAbuseCheck(ctx context.Context, _ []byte) error {
	month := time.Now().In(cnZone).Format("2006-01")
	threshold := trafficAbuseThresholdBytes()

	offenders, err := findTrafficAbusers(month, threshold)
	if err != nil {
		return fmt.Errorf("find traffic abusers: %w", err)
	}
	for _, o := range offenders {
		fresh, rerr := recordTrafficAbuseAlert(month, o.UserID, o.Bytes)
		if rerr != nil {
			log.Errorf(ctx, "[TRAFFIC-ABUSE] record user=%d: %v", o.UserID, rerr)
			continue
		}
		if !fresh {
			continue // already alerted this month
		}

		var user User
		email := "(unknown)"
		if uerr := db.Get().Preload("LoginIdentifies").Where("id = ?", o.UserID).First(&user).Error; uerr == nil {
			if e := getUserEmailFromIdentifies(&user); e != "" {
				email = e
			}
		}
		sendCloudSlackNotification(ctx, "Traffic Abuse Alert",
			fmt.Sprintf("user=%d email=%s month=%s used=%.1fGB threshold=%dGB",
				o.UserID, email, month, float64(o.Bytes)/float64(1<<30), threshold>>30))
	}

	if cerr := cleanupTrafficRetention(trafficRetentionDays); cerr != nil {
		log.Errorf(ctx, "[TRAFFIC-ABUSE] retention cleanup: %v", cerr)
	}
	return nil
}
