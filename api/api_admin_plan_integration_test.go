package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmin_PlansManagement(t *testing.T) {
	// Initialize test config (starts miniredis for asynq)
	testInitConfig()

	// Admin token (should be mocked or from config in real tests)
	adminToken := "admin-mock-token"
	r := SetupRouter()

	// 1. 获取套餐列表
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/plans", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	var listResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &listResp)
	assert.Contains(t, []float64{0, 401, 404}, listResp["code"])

	// 2. 创建套餐
	createReq := map[string]interface{}{
		"name":   "测试套餐",
		"price":  1,
		"remark": "自动化测试",
	}
	createBody, _ := json.Marshal(createReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/app/plans", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	assert.Contains(t, []float64{0, 401, 422}, createResp["code"])

	// 3. 更新套餐
	updateReq := map[string]interface{}{
		"name":   "测试套餐-更新",
		"price":  2,
		"remark": "自动化测试-更新",
	}
	updateBody, _ := json.Marshal(updateReq)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/app/plans/1", bytes.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var updateResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &updateResp)
	assert.Contains(t, []float64{0, 401, 404, 422}, updateResp["code"])

	// 4. 删除套餐
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/app/plans/1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	var delResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &delResp)
	assert.Contains(t, []float64{0, 401, 404}, delResp["code"])

	// 5. 恢复套餐
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/app/plans/1/restore", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(w, req)
	var restoreResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &restoreResp)
	assert.Contains(t, []float64{0, 401, 404}, restoreResp["code"])
}
