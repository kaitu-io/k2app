package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/lumberjack.v2"
)

// LogLevel is the global mutable log level. SetupLogging initializes it;
// SetLogLevel changes it at runtime without recreating the handler.
var LogLevel slog.LevelVar

// parseLevel converts a string log level to slog.Level.
// Defaults to slog.LevelInfo for unknown values.
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}


// systemLogPath returns the system-level log path (for root/admin).
func systemLogPath(name string) string {
	if runtime.GOOS == "windows" {
		progData := os.Getenv("ProgramData")
		if progData == "" {
			progData = `C:\ProgramData`
		}
		return filepath.Join(progData, "kaitu", name+".log")
	}
	// macOS and Linux: /var/log/kaitu/{name}.log
	return "/var/log/kaitu/" + name + ".log"
}

// userLogPath returns the user-level log path (for non-root).
func userLogPath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp dir if home is unavailable.
		return filepath.Join(os.TempDir(), name+".log")
	}

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "kaitu", "logs", name+".log")
	case "darwin":
		return filepath.Join(home, "Library", "Logs", "kaitu", name+".log")
	default:
		// Linux and others: ~/.local/share/kaitu/logs/{name}.log
		return filepath.Join(home, ".local", "share", "kaitu", "logs", name+".log")
	}
}

// DefaultLogPath returns the platform-appropriate log file path for the given
// process name, using the current process's effective UID to determine root vs user.
//
// Priority chain:
//  1. Root on Unix / admin on Windows → system dir (/var/log/kaitu/ or %ProgramData%)
//  2. Non-root → user dir (~/Library/Logs/kaitu/ on macOS, ~/.local/share/kaitu/logs/ on Linux, %LOCALAPPDATA% on Windows)
func DefaultLogPath(name string) string {
	if IsRoot() {
		return systemLogPath(name)
	}
	return userLogPath(name)
}

// SetupLogging configures the global slog default logger based on cfg.
// The output field controls destination:
//   - "stderr" → os.Stderr
//   - "stdout" → os.Stdout
//   - any other value → treated as a file path; lumberjack is used for rotation
//
// If output is empty, cfg.Output is used as-is; if still empty, "stderr" is the default.
// The level is set on the package-level LogLevel var, which can be changed at runtime
// via SetLogLevel without recreating the handler.
// Returns a cleanup function that restores the previous slog default handler and closes
// any open file writers.
func SetupLogging(cfg LogConfig, name string) (func(), error) {
	LogLevel.Set(parseLevel(cfg.Level))

	output := cfg.Output
	if output == "" {
		output = DefaultLogPath(name)
	}

	// Save the previous default handler so cleanup can restore it.
	prevHandler := slog.Default().Handler()

	var writer io.Writer
	var closeFn func()

	switch output {
	case "stderr":
		writer = os.Stderr
		closeFn = func() {}
	case "stdout":
		writer = os.Stdout
		closeFn = func() {}
	default:
		// Treat as file path — use lumberjack for log rotation.
		logPath := output
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			return nil, fmt.Errorf("log: create log dir: %w", err)
		}
		// Pre-create with world-readable permissions so non-root processes
		// (e.g. Tauri log upload) can read the file. Lumberjack copies mode
		// from existing file on rotation, so 0644 propagates automatically.
		if _, statErr := os.Stat(logPath); os.IsNotExist(statErr) {
			if f, createErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0644); createErr == nil {
				f.Close()
			}
		}
		lj := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    50, // MB
			MaxBackups: 3,
			MaxAge:     30, // days
		}
		writer = lj
		closeFn = func() {
			lj.Close() //nolint:errcheck
		}
	}

	opts := &slog.HandlerOptions{Level: &LogLevel}
	handler := slog.NewTextHandler(writer, opts)
	slog.SetDefault(slog.New(handler))

	cleanup := func() {
		slog.SetDefault(slog.New(prevHandler))
		closeFn()
	}

	return cleanup, nil
}

// SetLogLevel changes the global log level at runtime without recreating
// the handler or writer. Valid values: debug, info, warn, error.
func SetLogLevel(level string) {
	LogLevel.Set(parseLevel(level))
}
