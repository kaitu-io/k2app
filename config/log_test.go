package config

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestSystemLogPath verifies that systemLogPath returns a system-level path.
func TestSystemLogPath(t *testing.T) {
	path := systemLogPath("k2")
	if !strings.HasSuffix(path, "k2.log") {
		t.Errorf("path = %q, want suffix k2.log", path)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(path, "ProgramData") {
			t.Errorf("Windows system path = %q, want to contain ProgramData", path)
		}
	} else {
		if !strings.Contains(path, "/var/log/kaitu/") {
			t.Errorf("Unix system path = %q, want to contain /var/log/kaitu/", path)
		}
	}
}

// TestUserLogPath verifies that userLogPath returns a user-level path.
func TestUserLogPath(t *testing.T) {
	path := userLogPath("k2")
	if !strings.HasSuffix(path, "k2.log") {
		t.Errorf("path = %q, want suffix k2.log", path)
	}
	if runtime.GOOS != "windows" {
		if strings.Contains(path, "/var/log/kaitu/") {
			t.Errorf("user path = %q, should not be under /var/log/kaitu/", path)
		}
	}
}

// TestSetupLogging_DefaultCreatesFile verifies that empty output auto-detects
// and creates a log file via DefaultLogPath.
func TestSetupLogging_DefaultCreatesFile(t *testing.T) {
	// Use empty config — should auto-detect log path and create a file.
	cfg := LogConfig{} // Output is empty
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging with empty config: %v", err)
	}
	defer cleanup()

	// The auto-detected path should be a real file path (not "stderr" or "stdout").
	logPath := DefaultLogPath("k2")
	slog.Info("test auto-detect message")
	cleanup()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("auto-detected log file %q was not created", logPath)
	}
	// Clean up the auto-created file so tests don't leave artifacts.
	os.Remove(logPath)
}

// TestSetupLogging_Stderr verifies that output:"stderr" configures slog to use stderr.
func TestSetupLogging_Stderr(t *testing.T) {
	cfg := LogConfig{Level: "info", Output: "stderr"}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	// Should not error — stderr is always available.
	slog.Info("test message from stderr")
}

// TestSetupLogging_Stdout verifies that output:"stdout" configures slog to use stdout.
func TestSetupLogging_Stdout(t *testing.T) {
	cfg := LogConfig{Level: "info", Output: "stdout"}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	slog.Info("test message from stdout")
}

// TestSetupLogging_FileOverride verifies that output set to a file path creates the file.
func TestSetupLogging_FileOverride(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/test-k2-log.log"

	cfg := LogConfig{Level: "info", Output: logPath}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	slog.Info("test file message")

	// Flush by restoring default logger, then check file exists.
	cleanup()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("log file %q was not created", logPath)
	}
}

// TestSetupLogging_LevelDebug verifies that level:"debug" allows debug messages.
func TestSetupLogging_LevelDebug(t *testing.T) {
	cfg := LogConfig{Level: "debug", Output: "stderr"}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug level should be enabled, but handler rejected it")
	}
}

// TestSetupLogging_LevelError verifies that level:"error" filters info messages.
func TestSetupLogging_LevelError(t *testing.T) {
	cfg := LogConfig{Level: "error", Output: "stderr"}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	handler := slog.Default().Handler()
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info level should be disabled when level is error")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("error level should be enabled when level is error")
	}
}

// TestSetLogLevel_HotSwitch verifies that SetLogLevel changes the global log level
// at runtime without recreating the handler.
func TestSetLogLevel_HotSwitch(t *testing.T) {
	cfg := LogConfig{Level: "info", Output: "stderr"}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging: %v", err)
	}
	defer cleanup()

	// Initially info — debug should be disabled.
	handler := slog.Default().Handler()
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("debug should be disabled at info level")
	}

	// Hot-switch to debug.
	SetLogLevel("debug")
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("after SetLogLevel(debug), debug should be enabled")
	}

	// Hot-switch to error.
	SetLogLevel("error")
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("after SetLogLevel(error), info should be disabled")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("after SetLogLevel(error), error should be enabled")
	}

	// Unknown level defaults to info.
	SetLogLevel("unknown")
	if LogLevel.Level() != slog.LevelInfo {
		t.Errorf("SetLogLevel(unknown) = %v, want LevelInfo", LogLevel.Level())
	}
}

// TestSetupLogging_LumberjackConfig verifies that file output uses lumberjack writer
// (tested indirectly: SetupLogging must return no error for a file path).
func TestSetupLogging_LumberjackConfig(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/lumberjack-test.log"

	cfg := LogConfig{Level: "info", Output: logPath}
	cleanup, err := SetupLogging(cfg, "k2")
	if err != nil {
		t.Fatalf("SetupLogging with file path: %v", err)
	}
	defer cleanup()

	// Write multiple messages to exercise the writer.
	for i := 0; i < 5; i++ {
		slog.Info("lumberjack test", "i", i)
	}

	// Cleanup should close the lumberjack writer without error.
	cleanup()

	// File should have been written.
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("log file stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("log file is empty after writing messages")
	}
}

// TestSetupLogging_K2sName verifies that name="k2s" produces a path ending in k2s.log.
func TestSetupLogging_K2sName(t *testing.T) {
	path := DefaultLogPath("k2s")
	if !strings.HasSuffix(path, "k2s.log") {
		t.Errorf("path = %q, want suffix k2s.log", path)
	}
}
