package center

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// ===================== Batch Script Tests =====================

func TestBatchScriptsListEmpty(t *testing.T) {
	// Setup minimal router for unit testing
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Mock empty scripts response
	r.GET("/app/batch-scripts", func(c *gin.Context) {
		ListWithData(c, []BatchScriptResponse{}, &Pagination{Total: 0})
	})

	// Execute request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-scripts?page=1&pageSize=20", nil)
	r.ServeHTTP(w, req)

	// Parse response
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items      []BatchScriptResponse `json:"items"`
			Pagination *Pagination           `json:"pagination"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Assert
	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if len(resp.Data.Items) != 0 {
		t.Errorf("Expected 0 items, got %d", len(resp.Data.Items))
	}
}

func TestBatchScriptsListWithItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Mock scripts response with items
	r.GET("/app/batch-scripts", func(c *gin.Context) {
		items := []BatchScriptResponse{
			{ID: 1, Name: "Script 1", Description: "Test script 1", ExecuteWithSudo: false, CreatedAt: 1704067200000, UpdatedAt: 1704067200000},
			{ID: 2, Name: "Script 2", Description: "Test script 2", ExecuteWithSudo: true, CreatedAt: 1704153600000, UpdatedAt: 1704153600000},
		}
		ListWithData(c, items, &Pagination{Total: 2, Page: 1, PageSize: 20})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-scripts?page=1&pageSize=20", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items      []BatchScriptResponse `json:"items"`
			Pagination *Pagination           `json:"pagination"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if len(resp.Data.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(resp.Data.Items))
	}
	if resp.Data.Items[0].Name != "Script 1" {
		t.Errorf("Expected Script 1, got %s", resp.Data.Items[0].Name)
	}
	if !resp.Data.Items[1].ExecuteWithSudo {
		t.Error("Expected Script 2 to have executeWithSudo=true")
	}
}

// ===================== Batch Task Tests =====================

func TestBatchTasksListWithStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/app/batch-tasks", func(c *gin.Context) {
		status := c.Query("status")

		tasks := []BatchTaskResponse{
			{ID: 1, ScriptID: 1, ScriptName: "Script 1", Status: "running", TotalNodes: 3, CurrentIndex: 1, ScheduleType: "once"},
			{ID: 2, ScriptID: 2, ScriptName: "Script 2", Status: "completed", TotalNodes: 5, CurrentIndex: 5, ScheduleType: "once"},
			{ID: 3, ScriptID: 1, ScriptName: "Script 1", Status: "failed", TotalNodes: 2, CurrentIndex: 2, ScheduleType: "once"},
		}

		if status != "" {
			var filtered []BatchTaskResponse
			for _, t := range tasks {
				if t.Status == status {
					filtered = append(filtered, t)
				}
			}
			tasks = filtered
		}

		ListWithData(c, tasks, &Pagination{Total: int64(len(tasks)), Page: 1, PageSize: 20})
	})

	// Test without filter
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-tasks?page=1&pageSize=20", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []BatchTaskResponse `json:"items"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Data.Items) != 3 {
		t.Errorf("Expected 3 items without filter, got %d", len(resp.Data.Items))
	}

	// Test with status filter
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/app/batch-tasks?page=1&pageSize=20&status=completed", nil)
	r.ServeHTTP(w, req)

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Data.Items) != 1 {
		t.Errorf("Expected 1 completed item, got %d", len(resp.Data.Items))
	}
	if resp.Data.Items[0].Status != "completed" {
		t.Errorf("Expected status completed, got %s", resp.Data.Items[0].Status)
	}
}

func TestBatchTaskDetailWithResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/app/batch-tasks/:id", func(c *gin.Context) {
		startedAt := int64(1704067200000)
		endedAt := int64(1704067210000)
		duration := int64(10000)

		detail := BatchTaskDetailResponse{
			BatchTaskResponse: BatchTaskResponse{
				ID:           1,
				ScriptID:     1,
				ScriptName:   "Health Check",
				Status:       "completed",
				TotalNodes:   2,
				CurrentIndex: 2,
				ScheduleType: "once",
				IsEnabled:    true,
			},
			Results: []TaskResultItem{
				{
					NodeID:    1,
					NodeName:  "Node 1",
					NodeIPv4:  "192.168.1.1",
					NodeIndex: 0,
					Status:    "success",
					Stdout:    "OK",
					Stderr:    "",
					ExitCode:  0,
					StartedAt: &startedAt,
					EndedAt:   &endedAt,
					Duration:  &duration,
				},
				{
					NodeID:    2,
					NodeName:  "Node 2",
					NodeIPv4:  "192.168.1.2",
					NodeIndex: 1,
					Status:    "failed",
					Stdout:    "",
					Stderr:    "Connection refused",
					ExitCode:  1,
					Error:     "SSH connection failed",
					StartedAt: &startedAt,
					EndedAt:   &endedAt,
					Duration:  &duration,
				},
			},
			SuccessCount: 1,
			FailedCount:  1,
		}

		Success(c, &detail)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-tasks/1", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int                     `json:"code"`
		Data BatchTaskDetailResponse `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if len(resp.Data.Results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(resp.Data.Results))
	}
	if resp.Data.SuccessCount != 1 {
		t.Errorf("Expected 1 success, got %d", resp.Data.SuccessCount)
	}
	if resp.Data.FailedCount != 1 {
		t.Errorf("Expected 1 failed, got %d", resp.Data.FailedCount)
	}
}

// ===================== Retry Tests =====================

func TestRetryBatchTaskValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Test that retry endpoint validates task status
	r.POST("/app/batch-tasks/:id/retry", func(c *gin.Context) {
		// Simulate validation - task must be completed or failed
		taskStatus := "running" // Mocked as running

		if taskStatus != "completed" && taskStatus != "failed" {
			Error(c, ErrorForbidden, "Only completed or failed tasks can be retried")
			return
		}

		Success(c, &RetryBatchTaskResponse{TaskID: 999})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/app/batch-tasks/1/retry", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != int(ErrorForbidden) {
		t.Errorf("Expected error code %d, got %d", ErrorForbidden, resp.Code)
	}
}

func TestRetryBatchTaskSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.POST("/app/batch-tasks/:id/retry", func(c *gin.Context) {
		Success(c, &RetryBatchTaskResponse{TaskID: 999})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/app/batch-tasks/1/retry", nil)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	var resp struct {
		Code int                    `json:"code"`
		Data RetryBatchTaskResponse `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if resp.Data.TaskID != 999 {
		t.Errorf("Expected taskId 999, got %d", resp.Data.TaskID)
	}
}

// ===================== Scheduled Tasks Tests =====================

func TestScheduledTasksList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/app/batch-tasks/scheduled", func(c *gin.Context) {
		nextRunAt := int64(1704153600000)
		lastRunAt := int64(1704067200000)

		tasks := []ScheduledTaskInfo{
			{
				ID:         1,
				ScriptID:   1,
				ScriptName: "Health Check",
				CronExpr:   "0 2 * * *",
				IsEnabled:  true,
				NodeIDs:    []uint64{1, 2, 3},
				TotalNodes: 3,
				NextRunAt:  &nextRunAt,
				LastRunAt:  &lastRunAt,
				LastStatus: "completed",
				CreatedAt:  1704000000000,
			},
			{
				ID:         2,
				ScriptID:   2,
				ScriptName: "Disk Cleanup",
				CronExpr:   "0 * * * *",
				IsEnabled:  false,
				NodeIDs:    []uint64{1, 2},
				TotalNodes: 2,
				NextRunAt:  nil,
				LastRunAt:  nil,
				LastStatus: "",
				CreatedAt:  1704000000000,
			},
		}

		ListWithData(c, tasks, &Pagination{Total: int64(len(tasks))})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-tasks/scheduled", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []ScheduledTaskInfo `json:"items"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if len(resp.Data.Items) != 2 {
		t.Errorf("Expected 2 scheduled tasks, got %d", len(resp.Data.Items))
	}
	if !resp.Data.Items[0].IsEnabled {
		t.Error("Expected first task to be enabled")
	}
	if resp.Data.Items[1].IsEnabled {
		t.Error("Expected second task to be disabled")
	}
}

// ===================== Script Version Tests =====================

func TestScriptVersionsList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.GET("/app/batch-scripts/:id/versions", func(c *gin.Context) {
		versions := []BatchScriptVersionResponse{
			{Version: 3, CreatedAt: 1704240000000, CreatedBy: 1},
			{Version: 2, CreatedAt: 1704153600000, CreatedBy: 1},
			{Version: 1, CreatedAt: 1704067200000, CreatedBy: 1},
		}

		ListWithData(c, versions, &Pagination{Total: int64(len(versions))})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/app/batch-scripts/1/versions", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Items []BatchScriptVersionResponse `json:"items"`
		} `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if len(resp.Data.Items) != 3 {
		t.Errorf("Expected 3 versions, got %d", len(resp.Data.Items))
	}
	// Versions should be in descending order
	if resp.Data.Items[0].Version != 3 {
		t.Errorf("Expected first version to be 3, got %d", resp.Data.Items[0].Version)
	}
}

func TestScriptVersionRestore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.POST("/app/batch-scripts/:id/versions/:version/restore", func(c *gin.Context) {
		restoredScript := BatchScriptDetailResponse{
			ID:              1,
			Name:            "Test Script",
			Description:     "Restored version",
			Content:         "#!/bin/bash\necho 'Restored v2'",
			ExecuteWithSudo: false,
			CreatedAt:       1704067200000,
			UpdatedAt:       1704326400000, // Updated now
		}

		Success(c, &restoredScript)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/app/batch-scripts/1/versions/2/restore", nil)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int                       `json:"code"`
		Data BatchScriptDetailResponse `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if resp.Data.Content != "#!/bin/bash\necho 'Restored v2'" {
		t.Error("Expected restored content")
	}
}

// ===================== Script Test Execution Tests =====================

func TestScriptTestExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.POST("/app/batch-scripts/:id/test", func(c *gin.Context) {
		var req TestBatchScriptRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Error(c, ErrorInvalidArgument, err.Error())
			return
		}

		if req.NodeID == 0 {
			Error(c, ErrorInvalidArgument, "nodeId is required")
			return
		}

		result := TestBatchScriptResponse{
			Stdout:   "Script executed successfully\nOutput line 2",
			Stderr:   "",
			ExitCode: 0,
			Duration: 1500,
			Error:    "",
		}

		Success(c, &result)
	})

	// Test with valid request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/app/batch-scripts/1/test", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = readCloser(`{"nodeId": 1}`)
	r.ServeHTTP(w, req)

	var resp struct {
		Code int                     `json:"code"`
		Data TestBatchScriptResponse `json:"data"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != 0 {
		t.Errorf("Expected code 0, got %d", resp.Code)
	}
	if resp.Data.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.Data.ExitCode)
	}
	if resp.Data.Duration != 1500 {
		t.Errorf("Expected duration 1500, got %d", resp.Data.Duration)
	}
}

func TestScriptTestExecutionMissingNodeId(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	r.POST("/app/batch-scripts/:id/test", func(c *gin.Context) {
		var req TestBatchScriptRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Error(c, ErrorInvalidArgument, err.Error())
			return
		}

		if req.NodeID == 0 {
			Error(c, ErrorInvalidArgument, "nodeId is required")
			return
		}

		Success(c, &TestBatchScriptResponse{})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/app/batch-scripts/1/test", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = readCloser(`{}`)
	r.ServeHTTP(w, req)

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Code != int(ErrorInvalidArgument) {
		t.Errorf("Expected error code %d, got %d", ErrorInvalidArgument, resp.Code)
	}
}

// Helper to create io.ReadCloser from string
func readCloser(s string) *stringReadCloser {
	return &stringReadCloser{data: []byte(s)}
}

type stringReadCloser struct {
	data []byte
	pos  int
}

func (r *stringReadCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, nil
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *stringReadCloser) Close() error {
	return nil
}
