package center

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_register_device_log 注册设备日志元数据（S3 上传成功后客户端调用）
func api_register_device_log(c *gin.Context) {
	ctx := c.Request.Context()
	userID := ReqUserID(c)

	var req RegisterDeviceLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(ctx, "api_register_device_log: invalid request: %v", err)
		Error(c, ErrorInvalidArgument, "invalid request: "+err.Error())
		return
	}

	// Serialize meta to JSON string
	var metaStr string
	if req.Meta != nil {
		data, err := json.Marshal(req.Meta)
		if err != nil {
			log.Warnf(ctx, "api_register_device_log: failed to marshal meta: %v", err)
			Error(c, ErrorInvalidArgument, "invalid meta")
			return
		}
		metaStr = string(data)
	}

	// Create one DeviceLog per S3 key
	var userIDPtr *uint64
	if userID > 0 {
		userIDPtr = &userID
	}

	var feedbackIDPtr *string
	if req.FeedbackID != "" {
		feedbackIDPtr = &req.FeedbackID
	}

	logs := make([]DeviceLog, len(req.S3Keys))
	for i, key := range req.S3Keys {
		logs[i] = DeviceLog{
			UDID:       req.UDID,
			UserID:     userIDPtr,
			FeedbackID: feedbackIDPtr,
			S3Key:      key.S3Key,
			LogType:    key.Name,
			Reason:     req.Reason,
			Meta:       metaStr,
		}
	}

	if err := db.Get().Create(&logs).Error; err != nil {
		log.Errorf(ctx, "api_register_device_log: failed to save device logs: %v", err)
		Error(c, ErrorSystemError, "failed to save device logs")
		return
	}

	log.Infof(ctx, "api_register_device_log: saved %d log records for udid=%s reason=%s",
		len(logs), req.UDID, req.Reason)
	SuccessEmpty(c)
}
