package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestEvaluateHealth_Healthy(t *testing.T) {
	action := evaluateHealth(true, true, 0, 2)
	if action != actionNone {
		t.Fatalf("expected actionNone, got %d", action)
	}
}

func TestEvaluateHealth_NotRunning(t *testing.T) {
	action := evaluateHealth(false, false, 5, 2)
	if action != actionNone {
		t.Fatalf("expected actionNone for dead process, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveBelowThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 0, 2)
	if action != actionWait {
		t.Fatalf("expected actionWait, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveAtThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 1, 2)
	if action != actionCleanup {
		t.Fatalf("expected actionCleanup at threshold, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveAboveThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 5, 2)
	if action != actionCleanup {
		t.Fatalf("expected actionCleanup above threshold, got %d", action)
	}
}

func TestTryPing_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"message":"pong"}`))
	}))
	defer srv.Close()

	if !tryPing(srv.Listener.Addr().String()) {
		t.Fatal("expected ping to succeed")
	}
}

func TestTryPing_UnresponsiveServer(t *testing.T) {
	if tryPing("127.0.0.1:0") {
		t.Fatal("expected ping to fail on closed port")
	}
}

func TestFailureCount_ReadWriteReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failures")

	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	writeFailureCount(path, 3)
	if n := readFailureCount(path); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	resetFailureCount(path)
	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0 after reset, got %d", n)
	}
}

func TestFailureCount_StaleFileIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failures")

	writeFailureCount(path, 5)
	staleTime := time.Now().Add(-10 * time.Minute)
	os.Chtimes(path, staleTime, staleTime)

	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0 for stale file, got %d", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected stale file to be removed")
	}
}

func TestReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if pid := readPIDFile(path); pid != 0 {
		t.Fatalf("expected 0 for missing file, got %d", pid)
	}

	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
	if pid := readPIDFile(path); pid != os.Getpid() {
		t.Fatalf("expected %d, got %d", os.Getpid(), pid)
	}
}
