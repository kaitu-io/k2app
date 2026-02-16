package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// =====================================================================
// Device Transfer Unit Tests (No Database Required)
// =====================================================================

// TestDeviceTransferTemplate_Defined verifies that device transfer email template is defined
func TestDeviceTransferTemplate_Defined(t *testing.T) {
	assert.NotEmpty(t, deviceTransferTemplate.Subject, "Device transfer template subject should not be empty")
	assert.NotEmpty(t, deviceTransferTemplate.Body, "Device transfer template body should not be empty")

	// Verify subject text
	assert.Equal(t, "设备转移通知", deviceTransferTemplate.Subject)

	// Verify body contains required fields
	assert.Contains(t, deviceTransferTemplate.Body, "{{.TransferTime}}", "Body should contain TransferTime field")
	assert.Contains(t, deviceTransferTemplate.Body, "{{.DeviceRemark}}", "Body should contain DeviceRemark field")
	assert.Contains(t, deviceTransferTemplate.Body, "已被转移", "Body should mention device transfer")
}

// TestDeviceTransferMeta_Structure verifies DeviceTransferMeta structure
func TestDeviceTransferMeta_Structure(t *testing.T) {
	meta := DeviceTransferMeta{
		TransferTime: "2025-12-20 18:30:15",
		DeviceRemark: "Test Device",
	}

	assert.NotEmpty(t, meta.TransferTime, "TransferTime should not be empty")
	assert.NotEmpty(t, meta.DeviceRemark, "DeviceRemark should not be empty")
}

// TestDeviceKickTemplate_StillExists verifies existing template is not broken
func TestDeviceKickTemplate_StillExists(t *testing.T) {
	assert.NotEmpty(t, deviceKickTemplate.Subject, "Device kick template should still exist")
	assert.Equal(t, "设备已被移除通知", deviceKickTemplate.Subject)
}

// TestNewDeviceLoginTemplate_StillExists verifies existing template is not broken
func TestNewDeviceLoginTemplate_StillExists(t *testing.T) {
	assert.NotEmpty(t, newDeviceLoginTemplate.Subject, "New device login template should still exist")
	assert.Equal(t, "新设备登录提醒", newDeviceLoginTemplate.Subject)
}

// TestWebLoginTemplate_StillExists verifies existing template is not broken
func TestWebLoginTemplate_StillExists(t *testing.T) {
	assert.NotEmpty(t, webLoginTemplate.Subject, "Web login template should still exist")
	assert.Equal(t, "Web管理后台登录通知", webLoginTemplate.Subject)
}
