package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 线上 2026-11-08~10：桌面端上传轮转日志，文件名如 k2-2026-11-09T04-13-46.470.log.gz（35 字符）
// 被原样写进 log_type（当时 varchar(32)）→ 1406 Data too long → 整批 Create 失败，
// 同一次上传里的全部日志记录都丢了（7/7 次带长文件名的上传 500，16/16 次短文件名成功）。
func TestRegisterDeviceLog_LongRotatedFileNames(t *testing.T) {
	skipIfNoDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/user/device-log", api_register_device_log)

	udid := fmt.Sprintf("udid-longname-%d", time.Now().UnixNano())
	t.Cleanup(func() { db.Get().Unscoped().Where("udid = ?", udid).Delete(&DeviceLog{}) })

	type key struct {
		Name  string `json:"name"`
		S3Key string `json:"s3Key"`
	}
	names := []string{
		"k2.log",
		"k2-2026-11-09T04-13-46.470.log.gz", // 线上实际出现的形态
		strings.Repeat("x", 256),            // 新上限
	}
	keys := make([]key, 0, len(names))
	for i, n := range names {
		keys = append(keys, key{Name: n, S3Key: fmt.Sprintf("logs/%s/%d", udid, i)})
	}
	raw, err := json.Marshal(map[string]any{"udid": udid, "reason": "beta-auto-upload", "s3Keys": keys,
		"meta": map[string]any{"os": "macos", "appVersion": "0.4.16"}}) // 线上请求都带 meta
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/user/device-log", bytes.NewReader(raw)))
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Code *int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Code, "body=%s", w.Body.String())
	assert.Equal(t, 0, *resp.Code, "body=%s", w.Body.String())

	var got []DeviceLog
	require.NoError(t, db.Get().Where("udid = ?", udid).Order("id").Find(&got).Error)
	require.Len(t, got, len(names), "整批都要落库")
	for i, n := range names {
		assert.Equal(t, n, got[i].LogType)
	}
}
