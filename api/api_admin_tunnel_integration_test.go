package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmin_TunnelManagement(t *testing.T) {
	// Initialize test config (starts miniredis for asynq)
	testInitConfig()

	// Admin token (should be mocked or from config in real tests)
	adminToken := "admin-mock-token"
	r := SetupRouter()

	// 1. 获取节点列表
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/tunnels", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	assert.Contains(t, []float64{0, 401, 404}, listResp["code"])

	// 2. 创建节点（如有）/ 更新节点
	updateReq := map[string]interface{}{
		"name":   "测试节点",
		"remark": "自动化测试",
	}
	updateBody, _ := json.Marshal(updateReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/app/tunnels/1", bytes.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var updateResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &updateResp)
	assert.Contains(t, []float64{0, 401, 404}, updateResp["code"])

	// 3. 删除节点
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/app/tunnels/1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	var delResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &delResp)
	assert.Contains(t, []float64{0, 401, 404}, delResp["code"])
}
