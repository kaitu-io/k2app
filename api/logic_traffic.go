package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"context"

	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// Acct 设备计费使用情况
type Acct struct {
	UDID          string `json:"udid"`          // 设备ID
	SessionID     string `json:"sessionId"`     // 会话ID
	UploadBytes   int64  `json:"uploadBytes"`   // 本次分片上传字节数
	DownloadBytes int64  `json:"downloadBytes"` // 本次分片下载字节数
	SliceStartAt  int64  `json:"sliceStartAt"`  // 分片开始时间戳（秒）
	SliceEndAt    int64  `json:"sliceEndAt"`    // 分片结束时间戳（秒）
}

func flushSessionAcct(ctx context.Context, node *SlaveTunnel, accts []Acct) (overflowedDeviceIds []string, err error) {
	if len(accts) == 0 {
		return nil, nil
	}
	log.Infof(ctx, "flushing session acct for node %s, %d records", node.Name, len(accts))

	// 1. 将所有的设备的udid汇总出来
	udids := util.Map(accts, func(a Acct) string {
		return a.UDID
	})
	log.Debugf(ctx, "flushing acct for udids: %v", udids)

	// 2. 根据udids查询出devices，处理好 udid -> Device.ID 的关系
	devices := make([]Device, 0, len(accts))
	db.Get().Where("udid IN (?)", udids).Find(&devices)

	deviceToDeviceId := make(map[string]uint64, len(devices))
	deviceToUserId := make(map[string]uint64, len(devices))
	for _, device := range devices {
		deviceToDeviceId[device.UDID] = device.ID
		deviceToUserId[device.UDID] = device.UserID
	}
	log.Debugf(ctx, "found %d devices in DB for the given udids", len(devices))

	err = db.Get().Transaction(func(tx *gorm.DB) error {
		for _, acct := range accts {
			deviceID, exists := deviceToDeviceId[acct.UDID]
			if !exists {
				log.Warnf(ctx, "device with udid %s not found in DB, skipping acct record", acct.UDID)
				continue
			}
			userID := deviceToUserId[acct.UDID]
			sliceStart := acct.SliceStartAt
			sliceEnd := acct.SliceEndAt
			activeSeconds := sliceEnd - sliceStart
			sessionAcct := SessionAcct{
				UserID:       userID,
				DeviceID:     deviceID,
				SlaveID:      node.ID,
				SessionID:    acct.SessionID,
				InputBytes:   uint64(acct.DownloadBytes),
				OutputBytes:  uint64(acct.UploadBytes),
				Seconds:      activeSeconds,
				SliceStartAt: sliceStart,
				SliceEndAt:   sliceEnd,
			}
			log.Debugf(ctx, "插入 session 分片计费, session %s, 分片: %d-%d", acct.SessionID, sliceStart, sliceEnd)
			if err := tx.Create(&sessionAcct).Error; err != nil {
				log.Errorf(ctx, "failed to insert SessionAcct slice for session %s: %v", acct.SessionID, err)
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf(ctx, "transaction failed during flushSessionAcct: %v", err)
	}

	log.Infof(ctx, "successfully flushed session acct for node %s", node.Name)
	return overflowedDeviceIds, err
}
