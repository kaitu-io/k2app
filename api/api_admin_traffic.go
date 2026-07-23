package center

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// TrafficTopUser is one row of the monthly per-user traffic ranking.
type TrafficTopUser struct {
	UserID      uint   `json:"userId"`
	Email       string `json:"email"`
	UUID        string `json:"uuid"`
	RxBytes     int64  `json:"rxBytes"`
	TxBytes     int64  `json:"txBytes"`
	TotalBytes  int64  `json:"totalBytes"`
	DeviceCount int    `json:"deviceCount"`
	NodeCount   int    `json:"nodeCount"`
}

type TrafficTopUsersResponse struct {
	Month      string           `json:"month"`
	TotalBytes int64            `json:"totalBytes"` // 全网当月总量（含未识别桶）
	Users      []TrafficTopUser `json:"users"`
}

type TrafficDailyPoint struct {
	Date  string `json:"date"`
	Bytes int64  `json:"bytes"`
}

type TrafficBreakdownRow struct {
	Key   string `json:"key"` // udid 或 node ipv4
	Bytes int64  `json:"bytes"`
}

type TrafficUserDetailResponse struct {
	Month      string                `json:"month"`
	TotalBytes int64                 `json:"totalBytes"`
	Daily      []TrafficDailyPoint   `json:"daily"`
	Devices    []TrafficBreakdownRow `json:"devices"`
	Nodes      []TrafficBreakdownRow `json:"nodes"`
}

func queryTrafficTopUsers(month string, limit int) ([]TrafficTopUser, int64, error) {
	start, end, err := trafficMonthRange(month)
	if err != nil {
		return nil, 0, err
	}

	var rows []TrafficTopUser
	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("user_id, SUM(rx_bytes) AS rx_bytes, SUM(tx_bytes) AS tx_bytes, SUM(rx_bytes+tx_bytes) AS total_bytes, COUNT(DISTINCT udid) AS device_count, COUNT(DISTINCT node_ipv4) AS node_count").
		Where("date BETWEEN ? AND ?", start, end).
		Group("user_id").
		Order("total_bytes DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	var total struct{ Total int64 }
	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("COALESCE(SUM(rx_bytes+tx_bytes),0) AS total").
		Where("date BETWEEN ? AND ?", start, end).
		Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	// 附 email/uuid（user_id=0 保持空 = 「未识别设备」桶）
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		if r.UserID != 0 {
			ids = append(ids, r.UserID)
		}
	}
	if len(ids) > 0 {
		// UUID lives on User directly.
		var users []User
		if err := db.Get().Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, 0, err
		}
		uuidByID := map[uint]string{}
		for _, u := range users {
			uuidByID[uint(u.ID)] = u.UUID
		}

		// Email is not a User column — it lives on LoginIdentify (type="email"),
		// value stored via secretEncryptString/secretDecryptString (currently a
		// passthrough, see logic_secret.go TODO).
		var identifies []LoginIdentify
		if err := db.Get().Where("user_id IN ? AND type = ?", ids, "email").Find(&identifies).Error; err != nil {
			return nil, 0, err
		}
		emailByID := map[uint]string{}
		for _, li := range identifies {
			val, derr := secretDecryptString(context.Background(), li.EncryptedValue)
			if derr != nil {
				continue
			}
			emailByID[uint(li.UserID)] = val
		}

		for i := range rows {
			if uuid, ok := uuidByID[rows[i].UserID]; ok {
				rows[i].UUID = uuid
			}
			if email, ok := emailByID[rows[i].UserID]; ok {
				rows[i].Email = email
			}
		}
	}
	return rows, total.Total, nil
}

func queryTrafficUserDetail(userID uint, month string) (*TrafficUserDetailResponse, error) {
	start, end, err := trafficMonthRange(month)
	if err != nil {
		return nil, err
	}
	out := &TrafficUserDetailResponse{Month: month}

	var total struct{ Total int64 }
	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("COALESCE(SUM(rx_bytes+tx_bytes),0) AS total").
		Where("user_id = ? AND date BETWEEN ? AND ?", userID, start, end).
		Scan(&total).Error; err != nil {
		return nil, err
	}
	out.TotalBytes = total.Total

	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("date, SUM(rx_bytes+tx_bytes) AS bytes").
		Where("user_id = ? AND date BETWEEN ? AND ?", userID, start, end).
		Group("date").Order("date ASC").
		Scan(&out.Daily).Error; err != nil {
		return nil, err
	}
	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("udid AS `key`, SUM(rx_bytes+tx_bytes) AS bytes").
		Where("user_id = ? AND date BETWEEN ? AND ?", userID, start, end).
		Group("udid").Order("bytes DESC").
		Scan(&out.Devices).Error; err != nil {
		return nil, err
	}
	if err := db.Get().Model(&DeviceTrafficDaily{}).
		Select("node_ipv4 AS `key`, SUM(rx_bytes+tx_bytes) AS bytes").
		Where("user_id = ? AND date BETWEEN ? AND ?", userID, start, end).
		Group("node_ipv4").Order("bytes DESC").
		Scan(&out.Nodes).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func api_admin_traffic_top_users(c *gin.Context) {
	month := c.DefaultQuery("month", time.Now().In(cnZone).Format("2006-01"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if _, _, err := trafficMonthRange(month); err != nil {
		Error(c, ErrorInvalidArgument, "bad month")
		return
	}
	rows, total, err := queryTrafficTopUsers(month, limit)
	if err != nil {
		log.Errorf(c, "traffic top-users: %v", err)
		Error(c, ErrorSystemError, "query failed")
		return
	}
	Success(c, &TrafficTopUsersResponse{Month: month, TotalBytes: total, Users: rows})
}

func api_admin_traffic_user(c *gin.Context) {
	uuid := c.Query("uuid")
	if uuid == "" {
		Error(c, ErrorInvalidArgument, "uuid required")
		return
	}
	var user User
	if err := db.Get().Where("uuid = ?", uuid).First(&user).Error; err != nil {
		Error(c, ErrorNotFound, "user not found")
		return
	}
	month := c.DefaultQuery("month", time.Now().In(cnZone).Format("2006-01"))
	if _, _, err := trafficMonthRange(month); err != nil {
		Error(c, ErrorInvalidArgument, "bad month")
		return
	}
	detail, err := queryTrafficUserDetail(uint(user.ID), month)
	if err != nil {
		log.Errorf(c, "traffic user detail: %v", err)
		Error(c, ErrorSystemError, "query failed")
		return
	}
	Success(c, detail)
}
