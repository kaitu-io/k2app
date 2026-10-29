//go:build linux

package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// runtimeGOARCH is a seam for tests; defaults to runtime.GOARCH.
var runtimeGOARCH = func() string { return runtime.GOARCH }

// UpdateState is persisted to disk for crash recovery of an in-progress
// upgrade. Written before each stage transition.
type UpdateState struct {
	Stage     string `json:"stage"`     // downloading, verifying, replacing, restarting
	Version   string `json:"version"`   // target version
	TmpPath   string `json:"tmpPath"`   // download temp file
	BakPath   string `json:"bakPath"`   // backup path
	StartedAt int64  `json:"startedAt"` // Unix timestamp
}

// UpdateProgress is reported via the onProgress callback (typically fanned
// out as an SSE event).
type UpdateProgress struct {
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
}

// Upgrader manages OTA upgrades with persistent state recovery.
// A single Upgrader instance is safe for concurrent use; Apply() is
// serialized via an atomic flag (concurrent callers see an error).
type Upgrader struct {
	mu             deadlock.Mutex
	updating       atomic.Bool // prevents concurrent Apply()
	currentVersion string
	cfg            UpgradeConfig
	progress       UpdateProgress
	onProgress     func(UpdateProgress) // optional broadcast callback
}

// NewUpgrader constructs an Upgrader for the given running version and
// target-binary configuration.
func NewUpgrader(currentVersion string, cfg UpgradeConfig) *Upgrader {
	return &Upgrader{
		currentVersion: currentVersion,
		cfg:            cfg,
	}
}

// SetOnProgress installs a callback invoked on every progress transition.
// Typical implementations push the progress as an SSE event.
func (u *Upgrader) SetOnProgress(cb func(UpdateProgress)) {
	u.mu.Lock()
	u.onProgress = cb
	u.mu.Unlock()
}

// CurrentVersion returns the version string passed to NewUpgrader.
func (u *Upgrader) CurrentVersion() string {
	return u.currentVersion
}

// updateStatePath is derived from cfg.StateDir.
func (u *Upgrader) updateStatePath() string {
	return filepath.Join(u.cfg.StateDir, "update-state.json")
}

// RecoverOnStartup checks for interrupted update state and recovers.
// Called once before serving HTTP so that a crash mid-upgrade does not
// leave the filesystem in an inconsistent state.
func (u *Upgrader) RecoverOnStartup() {
	state := u.loadUpdateState()
	if state == nil {
		return
	}
	slog.Info("webui: recovering from interrupted update", "stage", state.Stage, "version", state.Version)

	binaryPath, _ := u.resolveBinaryPath()

	switch state.Stage {
	case "downloading", "verifying":
		// Interrupted before binary was replaced — clean up tmp file
		os.Remove(state.TmpPath)
		u.clearUpdateState()

	case "replacing":
		// Critical: binary may or may not have been replaced
		if u.currentVersion == state.Version {
			// New binary is running — update succeeded
			slog.Info("webui: update succeeded (post-replace recovery)", "version", state.Version)
			os.Remove(state.TmpPath)
			u.clearUpdateState()
		} else {
			// Replace may have failed — rollback from .bak if exists
			if _, err := os.Stat(state.BakPath); err == nil && binaryPath != "" {
				slog.Warn("webui: rolling back after interrupted replace", "bak", state.BakPath)
				if err := os.Rename(state.BakPath, binaryPath); err != nil {
					slog.Error("webui: rollback rename failed", "err", err, "bak", state.BakPath)
				}
				os.Chmod(binaryPath, 0755)
			}
			os.Remove(state.TmpPath)
			u.clearUpdateState()
		}

	case "restarting":
		// We ARE the new binary (restart succeeded, we're running)
		slog.Info("webui: update completed successfully (post-restart)", "version", state.Version)
		os.Remove(state.TmpPath)
		u.clearUpdateState()

	default:
		u.clearUpdateState()
	}
}

// Apply performs the OTA upgrade with persistent state at each stage.
// Only one Apply() can run at a time — concurrent calls return immediately.
// After a successful replace Apply() calls cfg.RestartFunc(); the running
// process is expected to be killed by the restart.
func (u *Upgrader) Apply() error {
	if !u.updating.CompareAndSwap(false, true) {
		return fmt.Errorf("update already in progress")
	}
	defer u.updating.Store(false)

	info, err := u.FetchLatest()
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if info.Version == u.currentVersion {
		return fmt.Errorf("already up to date")
	}

	binaryPath, err := u.resolveBinaryPath()
	if err != nil {
		return fmt.Errorf("resolve binary: %w", err)
	}

	expectedHash, ok := info.Checksums[u.BinaryFilename()]
	if !ok {
		return fmt.Errorf("no binary for linux/%s", runtimeGOARCH())
	}

	bakPath := binaryPath + ".bak"

	state := &UpdateState{
		Version:   info.Version,
		TmpPath:   fmt.Sprintf("/tmp/%s-update-%s", u.cfg.BinaryName, info.Version),
		BakPath:   bakPath,
		StartedAt: time.Now().Unix(),
	}

	// Stage 1: Backup current binary
	state.Stage = "downloading"
	u.saveUpdateState(state)
	u.setProgress("downloading", 0, "")

	if err := copyFile(binaryPath, bakPath); err != nil {
		u.clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("backup: %w", err)
	}

	// Stage 2: Download + verify + atomic replace
	state.Stage = "replacing"
	u.saveUpdateState(state)
	u.setProgress("replacing", 0, "")

	var lastErr error
	for _, ep := range u.cfg.ReleaseEndpoints {
		downloadURL := u.BinaryURL(ep, info.Version)
		if lastErr = DownloadAndReplace(downloadURL, expectedHash, binaryPath); lastErr == nil {
			break
		}
		slog.Warn("webui: download failed, trying next endpoint", "url", downloadURL, "err", lastErr)
	}
	if lastErr != nil {
		slog.Warn("webui: all endpoints failed, rolling back", "err", lastErr)
		os.Rename(bakPath, binaryPath)
		u.clearUpdateState()
		u.setProgress("error", 0, lastErr.Error())
		return fmt.Errorf("download+replace: %w", lastErr)
	}

	// Stage 3: Restart (this process is killed by the restart)
	state.Stage = "restarting"
	u.saveUpdateState(state)
	u.setProgress("restarting", 0, "")

	u.Restart()

	u.setProgress("done", 1, "")
	return nil
}

func (u *Upgrader) setProgress(stage string, progress float64, errMsg string) {
	u.mu.Lock()
	u.progress = UpdateProgress{Stage: stage, Progress: progress, Error: errMsg}
	p := u.progress
	cb := u.onProgress
	u.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

// --- Update state persistence (under cfg.StateDir) ---

func (u *Upgrader) saveUpdateState(state *UpdateState) {
	data, err := json.Marshal(state)
	if err != nil {
		slog.Warn("webui: failed to marshal update state", "err", err)
		return
	}
	if err := os.MkdirAll(u.cfg.StateDir, 0700); err != nil {
		slog.Warn("webui: failed to create state dir for update", "err", err)
		return
	}
	if err := os.WriteFile(u.updateStatePath(), data, 0644); err != nil {
		slog.Warn("webui: failed to write update state", "err", err)
	}
}

func (u *Upgrader) loadUpdateState() *UpdateState {
	data, err := os.ReadFile(u.updateStatePath())
	if err != nil {
		return nil
	}
	var state UpdateState
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	if time.Since(time.Unix(state.StartedAt, 0)) > time.Hour {
		slog.Warn("webui: expiring stale update state", "age", time.Since(time.Unix(state.StartedAt, 0)))
		u.clearUpdateState()
		return nil
	}
	return &state
}

func (u *Upgrader) clearUpdateState() {
	os.Remove(u.updateStatePath())
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
