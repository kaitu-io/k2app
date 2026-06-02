package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"

	"gopkg.in/lumberjack.v2"
)

// LogLevel is the global mutable log level. SetupLogging initializes it;
// SetLogLevel changes it at runtime without recreating the handler.
var LogLevel slog.LevelVar

// buildLogLevel is the default log level when none is explicitly configured.
// Set at build time via ldflags:
//
//	go build -ldflags "-X github.com/kaitu-io/k2/config.buildLogLevel=info"
//
// Default "debug" for development. CI/Makefile passes K2_BUILD_LOG_LEVEL env.
var buildLogLevel = "debug"

// activeStderrRedirect holds the cleanup function for the current stderr→lumberjack
// redirect goroutine. Since fd2 is process-global (one per process), only one
// redirect can be active. SetupLogging auto-cleans the previous one before
// creating a new one; CloseLogging cleans the current one for process shutdown.
var (
	stderrMu              sync.Mutex
	activeStderrCleanupFn func() // nil when no redirect is active
	stderrGeneration      uint64 // incremented each time a new redirect is created
)

// closeActiveStderr tears down the current stderr redirect goroutine if one exists.
// Must be called with stderrMu held.
func closeActiveStderr() {
	if activeStderrCleanupFn != nil {
		activeStderrCleanupFn()
		activeStderrCleanupFn = nil
	}
}

// CloseLogging tears down the active stderr→lumberjack redirect goroutine.
// Safe to call multiple times (idempotent). Use in process shutdown or test cleanup.
func CloseLogging() {
	stderrMu.Lock()
	defer stderrMu.Unlock()
	closeActiveStderr()
}

// parseLevel converts a string log level to slog.Level.
// When level is empty, falls back to buildLogLevel (set via ldflags).
func parseLevel(level string) slog.Level {
	if level == "" {
		level = buildLogLevel
	}
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
			MaxSize:    20, // MB
			MaxBackups: 3,
			MaxAge:     7, // days
			Compress:   true,
		}

		// Redirect os.Stderr to lumberjack so go-deadlock output,
		// runtime panics, and CGo crashes appear inline in k2.log.
		//
		// fd2 is process-global, so only one redirect can be active.
		// Auto-clean previous redirect before creating a new one.
		stderrMu.Lock()
		closeActiveStderr()

		var stderrDone chan struct{}
		var restoreStderr func()
		stderrR, stderrW, pipeErr := os.Pipe()
		if pipeErr == nil {
			os.Stderr = stderrW
			restoreStderr, _ = redirectStderrFD(stderrW) // also redirect fd 2 for runtime/CGo
			stderrDone = make(chan struct{})
			ljForStderr := lj // capture for goroutine
			// NO recover() here — intentional. If this goroutine panics, the process
			// should crash rather than leave a dead pipe reader. A dead reader causes
			// the pipe buffer (64KB) to fill, blocking ALL stderr writes including
			// Go runtime fatal error output, turning the process into a zombie.
			// lumberjack.Write() does not panic in practice (pure file I/O + mutex).
			go func() {
				defer close(stderrDone)
				buf := make([]byte, 4096)
				for {
					n, err := stderrR.Read(buf)
					if n > 0 {
						ljForStderr.Write(buf[:n]) //nolint:errcheck
					}
					if err != nil {
						return
					}
				}
			}()
			stderrGeneration++
			activeStderrCleanupFn = func() {
				if restoreStderr != nil {
					restoreStderr()
				}
				stderrW.Close()
				<-stderrDone
				stderrR.Close()
			}
		}
		// Capture the generation while still holding stderrMu to avoid a
		// data race with concurrent SetupLogging calls.
		myGen := stderrGeneration
		stderrMu.Unlock()

		if cfg.ExtraWriter != nil {
			writer = io.MultiWriter(lj, cfg.ExtraWriter)
		} else {
			writer = lj
		}
		closeFn = func() {
			stderrMu.Lock()
			if stderrGeneration == myGen && activeStderrCleanupFn != nil {
				activeStderrCleanupFn()
				activeStderrCleanupFn = nil
			}
			stderrMu.Unlock()
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

// SetupCrashOutput tells the Go runtime to ALSO write unhandled panics and
// fatal runtime errors directly to {logDir}/k2-crash.log, in addition to stderr.
//
// Why this is separate from SetupLogging: on iOS/Android, SetupLogging routes
// stderr (and thus runtime crash output) through a pipe + reader goroutine into
// lumberjack. That path races process exit — a fatal panic's stack can be stuck
// in the pipe buffer and never reach k2.log, so NE crashes were undiagnosable.
// debug.SetCrashOutput makes the runtime write the crash report straight to a
// durable file descriptor (no pipe, no goroutine), so the next uploaded feedback
// bundle contains the stack. The file is named k2-crash.log so mobile uploadLogs
// (which globs *.log) ships it automatically.
//
// Empty logDir is a no-op (desktop already gets crashes inline via the stderr
// redirect; this targets the memory-constrained mobile NE/Service processes).
// SetCrashOutput duplicates the fd, so the local handle is closed immediately.
func SetupCrashOutput(logDir string) error {
	if logDir == "" {
		return nil
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("crash output: create dir: %w", err)
	}
	path := filepath.Join(logDir, "k2-crash.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("crash output: open %s: %w", path, err)
	}
	defer f.Close() // SetCrashOutput dups the fd; ours is no longer needed.
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return fmt.Errorf("crash output: SetCrashOutput: %w", err)
	}
	return nil
}

// SetLogLevel changes the global log level at runtime without recreating
// the handler or writer. Valid values: debug, info, warn, error.
func SetLogLevel(level string) {
	LogLevel.Set(parseLevel(level))
}
