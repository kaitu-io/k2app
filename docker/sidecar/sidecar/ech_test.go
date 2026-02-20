package sidecar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchECHKeys_Success(t *testing.T) {
	// Create a test server that returns ECH keys
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/slave/ech/keys" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify Basic Auth
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("missing Basic Auth")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if username != "1.2.3.4" || password != "test-secret" {
			t.Errorf("wrong credentials: %s:%s", username, password)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Return mock ECH keys
		resp := CenterResponse[echKeysListData]{
			Code:    0,
			Message: "success",
			Data: &echKeysListData{
				Items: []ECHKeyConfig{
					{
						ConfigID:   7,
						PrivateKey: "dGVzdF9wcml2YXRl",
						PublicKey:  "dGVzdF9wdWJsaWM=",
						KEMId:      32,
						KDFId:      1,
						AEADId:     1,
						Status:     "active",
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create a node with the test server URL
	node := &Node{
		CenterURL: server.URL,
		Secret:    "test-secret",
		IPv4:      "1.2.3.4",
		Country:   "US",
	}

	// Create temp directory for output
	tmpDir, err := os.MkdirTemp("", "ech_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "ech_keys.yaml")

	// Fetch ECH keys
	count, err := node.FetchECHKeys(outputPath)
	if err != nil {
		t.Fatalf("FetchECHKeys failed: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 key, got %d", count)
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("ECH keys file was not created")
	}

	// Verify file contents
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read ECH keys file: %v", err)
	}

	content := string(data)
	if !contains(content, "config_id: 7") {
		t.Error("ECH keys file missing config_id")
	}
	if !contains(content, "status: active") {
		t.Error("ECH keys file missing status")
	}
}

func TestFetchECHKeys_APIError(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CenterResponse[echKeysListData]{
			Code:    1001,
			Message: "unauthorized",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	node := &Node{
		CenterURL: server.URL,
		Secret:    "test-secret",
		IPv4:      "1.2.3.4",
		Country:   "US",
	}

	tmpDir, err := os.MkdirTemp("", "ech_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "ech_keys.yaml")

	// Fetch should fail
	_, err = node.FetchECHKeys(outputPath)
	if err == nil {
		t.Error("expected error for API error response")
	}
}

func TestFetchECHKeys_NoKeys(t *testing.T) {
	// Create a test server that returns no keys
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CenterResponse[echKeysListData]{
			Code:    0,
			Message: "success",
			Data: &echKeysListData{
				Items: []ECHKeyConfig{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	node := &Node{
		CenterURL: server.URL,
		Secret:    "test-secret",
		IPv4:      "1.2.3.4",
		Country:   "US",
	}

	tmpDir, err := os.MkdirTemp("", "ech_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	outputPath := filepath.Join(tmpDir, "ech_keys.yaml")

	// Fetch should succeed but return 0 keys
	count, err := node.FetchECHKeys(outputPath)
	if err != nil {
		t.Fatalf("FetchECHKeys failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 keys, got %d", count)
	}
}

func TestSendSIGHUP_FileNotFound(t *testing.T) {
	err := SendSIGHUP("/nonexistent/path/k2-slave.pid")
	if err == nil {
		t.Error("expected error for nonexistent PID file")
	}
}

func TestSendSIGHUP_InvalidPID(t *testing.T) {
	// Create temp file with invalid PID
	tmpFile, err := os.CreateTemp("", "pid_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString("not-a-number"); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	err = SendSIGHUP(tmpFile.Name())
	if err == nil {
		t.Error("expected error for invalid PID")
	}
}

// contains checks if substr is in s
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
