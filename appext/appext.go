package appext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/safego"
)

// logLevel is the global mutable log level for mobile builds.
// Using slog.LevelVar allows runtime changes without recreating the handler.
var logLevel slog.LevelVar

func init() {
	// Print ALL goroutine stacks on unrecoverable panics (mobile equivalent of
	// daemon's HandlePanic). Ensures logcat/os_log gets full Go stack traces.
	debug.SetTraceback("crash")
	logLevel.Set(slog.LevelDebug)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &logLevel})))
}

// SetLogLevel changes the global slog level at runtime.
// Valid values: "debug", "info", "warn", "error". Unknown defaults to "info".
// gomobile exports this as AppextSetLogLevel (ObjC) / Appext.setLogLevel (Java).
func SetLogLevel(level string) {
	switch level {
	case "debug":
		logLevel.Set(slog.LevelDebug)
	case "warn", "warning":
		logLevel.Set(slog.LevelWarn)
	case "error":
		logLevel.Set(slog.LevelError)
	default:
		logLevel.Set(slog.LevelInfo)
	}
	slog.Info("appext: log level changed", "level", level)
}

// FreeMemory forces a Go GC cycle and returns freed memory to the OS.
// Call after Pause() or other resource-release events to reduce RSS on mobile.
// gomobile exports this as AppextFreeMemory (ObjC) / Appext.freeMemory (Java).
func FreeMemory() {
	debug.FreeOSMemory()
}

// Engine is the mobile VPN engine. It delegates all tunnel assembly and
// lifecycle management to engine.Engine, exposing only the gomobile-compatible
// API surface (string, int, int64 params only).
type Engine struct {
	inner       *engine.Engine
	platformNet *nePlatform // Darwin NE: owns monitors, closed on Stop()
}

// NewEngine creates a new Engine in the disconnected state.
func NewEngine() *Engine {
	return &Engine{
		inner: engine.New(),
	}
}

// SetEventHandler registers a callback receiver for state changes,
// errors, and traffic stats. Pass nil to clear.
func (e *Engine) SetEventHandler(h EventHandler) {
	if h == nil {
		e.inner.SetEventHandler(nil)
		return
	}
	e.inner.SetEventHandler(&eventBridge{handler: h})
}

// Status returns the current engine state.
func (e *Engine) Status() string {
	return e.inner.Status()
}

// Start assembles the tunnel stack and begins forwarding traffic.
// configJSON is a JSON-encoded config.ClientConfig; fd is the TUN file
// descriptor provided by the platform (iOS NEPacketTunnelProvider /
// Android VpnService); cfg provides platform-specific paths (CacheDir, LogDir).
func (e *Engine) Start(configJSON string, fd int, cfg *EngineConfig) (err error) {
	// Track dup'd FD for cleanup on early failure (before engine.Start takes ownership).
	dupFd := -1
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in Start", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("engine: panic in Start: %v", r)
		}
		if err != nil && dupFd >= 0 {
			syscall.Close(dupFd)
		}
	}()
	if cfg == nil {
		return fmt.Errorf("engine: EngineConfig is required")
	}

	// Duplicate the TUN FD so k2 owns it independently of the native wrapper.
	// The native side (NEPacketTunnelProvider / VpnService) may close its copy
	// at any time; dup isolates us from that lifecycle.
	if fd > 0 {
		newFd, dupErr := syscall.Dup(fd)
		if dupErr != nil {
			return fmt.Errorf("dup TUN fd: %w", dupErr)
		}
		fd = newFd
		dupFd = newFd
	}

	var clientCfg config.ClientConfig
	if err := json.Unmarshal([]byte(configJSON), &clientCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	config.SetDefaults(&clientCfg)

	// Configure logging from client config (default: info to stderr).
	logCfg := config.LogConfig{Level: clientCfg.Log.Level, Output: "stderr"}
	if cfg.LogDir != "" {
		logCfg.Output = filepath.Join(cfg.LogDir, "k2.log")
		if cfg.Debug {
			logCfg.ExtraWriter = os.Stderr // dual output: file + stderr (Xcode console / logcat)
		}
	}
	if _, err := config.SetupLogging(logCfg, "k2"); err != nil {
		slog.Warn("appext: SetupLogging failed, using defaults", "err", err)
	}

	// Route Go fatal/unrecovered panics straight to {LogDir}/k2-crash.log via the
	// runtime (not the stderr→pipe→lumberjack path, which races process exit on a
	// memory-constrained NE). Ensures a Go-side crash stack reaches the next
	// uploaded feedback bundle. Recovered panics already slog to k2.log. Best-effort.
	if cfg.LogDir != "" {
		if err := config.SetupCrashOutput(cfg.LogDir); err != nil {
			slog.Warn("appext: SetupCrashOutput failed", "err", err)
		}
	}

	// Diagnostic: verify k2.log is writable after SetupLogging.
	// Write results to native.log (known to be uploadable) instead of stderr.
	if cfg.LogDir != "" {
		k2logPath := filepath.Join(cfg.LogDir, "k2.log")
		nativeLogPath := filepath.Join(cfg.LogDir, "native.log")
		diagLog := func(msg string) {
			// Append to native.log in NativeLogger-compatible format
			if f, err := os.OpenFile(nativeLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
				fmt.Fprintf(f, "[%s] [INFO] %s\n", time.Now().UTC().Format(time.RFC3339), msg)
				f.Close()
			}
			fmt.Fprintf(os.Stderr, "appext: %s\n", msg)
		}

		if info, statErr := os.Stat(k2logPath); statErr != nil {
			diagLog(fmt.Sprintf("DIAG k2.log stat failed: %v", statErr))
		} else {
			diagLog(fmt.Sprintf("DIAG k2.log exists size=%d mode=%s", info.Size(), info.Mode()))
		}
		// Direct write test to verify POSIX file I/O in iOS sandbox
		if f, openErr := os.OpenFile(k2logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); openErr != nil {
			diagLog(fmt.Sprintf("DIAG k2.log open FAILED: %v", openErr))
		} else {
			_, writeErr := f.WriteString("appext: DIAG direct write test\n")
			f.Close()
			if writeErr != nil {
				diagLog(fmt.Sprintf("DIAG k2.log write FAILED: %v", writeErr))
			} else {
				diagLog("DIAG k2.log direct write OK")
			}
		}
	}

	// Inject platform cache dir for rule bundles.
	if cfg.CacheDir != "" {
		clientCfg.CacheDir = filepath.Join(cfg.CacheDir, "rules")
	}

	ecfg := engine.Config{
		Client:           &clientCfg,
		FileDescriptor:   fd,
		NetworkExtension: isNetworkExtension,
		// Mobile radio wake-ups are expensive — rely on protocol keepalive
		// instead of actively echo-probing the inactive backup transport
		// (engine/health.go echoProbeLoop, Task 7 dual-transport scoreboard).
		MobilePowerSaving: true,
	}
	if cfg.SocketProtector != nil {
		p := cfg.SocketProtector
		ecfg.DirectDialer = &protectedDialer{
			protect: func(socketFd int) bool {
				if socketFd < 0 || socketFd > math.MaxInt32 {
					slog.Warn("socket protect: fd out of int32 range", "fd", socketFd)
					return false
				}
				ok := p.Protect(int32(socketFd))
				slog.Debug("appext: SocketProtector.Protect()", "fd", socketFd, "ok", ok)
				return ok
			},
		}
	} else if isNetworkExtension {
		// Darwin NE: unified platform networking (DirectDialer + NetworkMonitor + InterfaceMonitor).
		// All three roles share one DefaultInterfaceMonitor with UnderNetworkExtension: true.
		if pn, err := newNEPlatform(); err == nil && pn != nil {
			ecfg.DirectDialer = pn
			ecfg.NetworkMonitor = pn
			ecfg.InterfaceMonitor = pn.InterfaceMonitor()
			e.platformNet = pn
			slog.Info("appext: NE platform networking active", "interface", pn.interfaceName())
		} else if err != nil {
			slog.Warn("appext: NE platform init failed, relying on NECP", "err", err)
		}
	}

	// Wire process attribution (Android only): ConnectionOwner (owner-UID
	// platform API) + PackageResolver (UID→packages) → ProcessSearcher.
	if ps := makeProcessSearcher(cfg); ps != nil {
		ecfg.ProcessSearcher = ps
	}

	slog.Info("appext: start",
		"server", clientCfg.Server,
		"allProxy", clientCfg.IsAllProxy(),
		"hasDirectDialer", ecfg.DirectDialer != nil,
		"hasNetworkMonitor", ecfg.NetworkMonitor != nil,
		"hasInterfaceMonitor", ecfg.InterfaceMonitor != nil,
		"isNE", isNetworkExtension)

	// Ownership transfers to engine.Start — its failure guard closes the FD on
	// every error return (whether or not sing-tun consumed it). Clear dupFd so
	// the deferred cleanup here does NOT double-close a potentially reused FD.
	dupFd = -1
	if err := e.inner.Start(ecfg); err != nil {
		return err
	}
	// Reclaim init-time allocations (config parsing, k2rule loading, provider setup).
	// On iOS NE with ~50MB jetsam, every MB counts.
	debug.FreeOSMemory()
	return nil
}

// Pause releases wire transport resources for memory conservation (iOS).
// The provider and gVisor stack remain alive. Call Wake() to restore.
func (e *Engine) Pause() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in Pause", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	e.inner.Pause()
}

// Wake restores the engine after a Pause, forcing fresh transport connections.
func (e *Engine) Wake() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in Wake", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	e.inner.Wake()
}

// Stop tears down the tunnel and returns the engine to disconnected.
func (e *Engine) Stop() (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in Stop", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("engine: panic in Stop: %v", r)
		}
	}()
	err = e.inner.Stop() // engine.Stop() closes NetworkMonitor (nePlatform) internally
	if e.platformNet != nil {
		e.platformNet = nil
	}
	return
}

// NotifyNetEvent receives a network state change from the platform layer.
// Call from iOS NWPathMonitor or Android ConnectivityManager.
// event.Signal: "available", "unavailable", or "changed".
// See NetEvent for field documentation.
func (e *Engine) NotifyNetEvent(event *NetEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in NotifyNetEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	if event == nil {
		slog.Warn("appext: NotifyNetEvent called with nil event")
		return
	}
	e.inner.OnNetEvent(&engine.NetEvent{
		Signal:         event.Signal,
		InterfaceName:  event.InterfaceName,
		InterfaceIndex: event.InterfaceIndex,
		IsWifi:         event.IsWifi,
		IsCellular:     event.IsCellular,
		HasIPv4:        event.HasIPv4,
		HasIPv6:        event.HasIPv6,
		Source:         event.Source,
	})
}

// PrefetchRules refreshes the rule bundle cache in the background. Call at
// app/service entry (Android MainApplication, iOS AppDelegate) so a newer rule
// set is usually on disk before the user connects. engine.Start never waits
// for it (#3889): the connect path is local-only and serves whatever is on
// disk; if this lands a new set after the engine loaded the old one, the
// engine's post-connect updater sees the on-disk set change and hot-reloads.
//
// cacheDir is the base directory (bundles live in cacheDir + "/rules"),
// matching the layout engine.Start uses via clientCfg.CacheDir.
//
// Safe for multiple concurrent calls: EnsureBundles internally dedupes via
// singleflight.
func PrefetchRules(cacheDir string) {
	if cacheDir == "" {
		return
	}
	dir := filepath.Join(cacheDir, "rules")

	safego.Go(func() {
		if err := rule.SeedFromEmbedIfEmpty(dir); err != nil {
			slog.Warn("appext: seed from embed failed", "err", err, "dir", dir)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
			slog.Warn("appext: PrefetchRules failed", "err", err)
			return
		}
		slog.Info("appext: PrefetchRules complete", "dir", dir)
	})
}

// StatusJSON returns the engine status as a JSON string.
// The format matches daemon.statusInfo() for consistency.
func (e *Engine) StatusJSON() (result string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in StatusJSON", "panic", r, "stack", string(debug.Stack()))
			result = `{"state":"disconnected"}`
		}
	}()
	return e.inner.StatusJSON()
}
