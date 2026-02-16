package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// api_get_devices 获取设备列表
//
func api_get_devices(c *gin.Context) {
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting to get devices", userID)

	var devices []Device
	err := db.Get().
		Where("user_id = ?", userID).Find(&devices).Error
	if err != nil {
		log.Errorf(c, "failed to get devices for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to get user")
		return
	}

	deviceList := make([]DataDevice, 0)
	for _, device := range devices {
		deviceList = append(deviceList, DataDevice{
			UDID:            device.UDID,
			Remark:          device.Remark,
			TokenLastUsedAt: device.TokenLastUsedAt,
		})
	}

	log.Infof(c, "successfully retrieved %d devices for user %d", len(deviceList), userID)
	ItemsAll(c, deviceList)
}

// api_delete_device 删除设备
//
func api_delete_device(c *gin.Context) {
	uuid := c.Param("uuid")
	currentDeviceID := ReqUDID(c)
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting to delete device %s", userID, uuid)

	if currentDeviceID == uuid {
		log.Warnf(c, "user %d attempted to delete current device %s", userID, uuid)
		Error(c, ErrorInvalidOperation, "cannot delete current device")
		return
	}

	result := db.Get().Where("udid = ? and user_id = ?", uuid, userID).Delete(&Device{})
	if result.Error != nil {
		log.Errorf(c, "failed to delete device %s for user %d: %v", uuid, userID, result.Error)
		Error(c, ErrorSystemError, "failed to delete device")
		return
	}
	if result.RowsAffected == 0 {
		log.Warnf(c, "device %s not found for user %d to delete", uuid, userID)
		Error(c, ErrorNotFound, "device not found or not owned by user")
		return
	}

	log.Infof(c, "user %d successfully deleted device %s", userID, uuid)
	SuccessEmpty(c)
}

// UpdateDeviceRemarkRequest 更新设备备注请求数据结构
//
type UpdateDeviceRemarkRequest struct {
	Remark string `json:"remark" example:"我的设备"` // 设备备注
}

// api_update_device_remark 更新设备备注
//
func api_update_device_remark(c *gin.Context) {
	uuid := c.Param("uuid")
	userID := ReqUserID(c)
	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request body for updating device %s remark by user %d: %v", uuid, userID, err)
		Error(c, ErrorInvalidArgument, "invalid request body")
		return
	}
	log.Infof(c, "user %d requesting to update remark for device %s", userID, uuid)

	var device Device
	err := db.Get().Where("udid = ? AND user_id = ?", uuid, userID).First(&device).Error
	if err != nil {
		log.Warnf(c, "device %s not found for user %d to update remark", uuid, userID)
		Error(c, ErrorNotFound, "device not found")
		return
	}

	device.Remark = req.Remark
	if err := db.Get().Save(&device).Error; err != nil {
		log.Errorf(c, "failed to update device %s remark for user %d: %v", uuid, userID, err)
		Error(c, ErrorSystemError, "failed to update device remark")
		return
	}

	log.Infof(c, "user %d successfully updated remark for device %s", userID, uuid)
	SuccessEmpty(c)
}
