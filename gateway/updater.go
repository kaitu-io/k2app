//go:build linux

package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/sasha-s/go-deadlock"
)

var updateStatePath = "/etc/k2r/update-state.json"

// UpdateState persisted to disk for crash recovery.
type UpdateState struct {
	Stage     string `json:"stage"`     // last stage: downloading, verifying, replacing, restarting
	Version   string `json:"version"`   // target version
	TmpPath   string `json:"tmpPath"`   // download temp file
	BakPath   string `json:"bakPath"`   // backup path
	StartedAt int64  `json:"startedAt"` // Unix timestamp
}

// UpdateProgress reported via SSE.
type UpdateProgress struct {
	Stage    string  `json:"stage"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
}

// Updater manages OTA updates with persistent state recovery.
type Updater struct {
	mu             deadlock.Mutex
	currentVersion string
	binPath        string
	progress       UpdateProgress
	onProgress     func(UpdateProgress) // SSE broadcast callback
}

// NewUpdater creates an updater for the running binary.
func NewUpdater(version string) *Updater {
	binPath, err := selfExePath()
	if err != nil {
		binPath = "/usr/bin/k2r" // fallback
	}
	return &Updater{
		currentVersion: version,
		binPath:        binPath,
	}
}

// RecoverOnStartup checks for interrupted update state and recovers.
// Called once during Gateway.Run() before serving HTTP.
func (u *Updater) RecoverOnStartup() {
	state := loadUpdateState()
	if state == nil {
		return
	}
	slog.Info("gateway: recovering from interrupted update", "stage", state.Stage, "version", state.Version)

	switch state.Stage {
	case "downloading", "verifying":
		// Interrupted before binary was replaced — clean up tmp file
		os.Remove(state.TmpPath)
		clearUpdateState()

	case "replacing":
		// Critical: binary may or may not have been replaced
		if u.currentVersion == state.Version {
			// New binary is running — update succeeded
			slog.Info("gateway: update succeeded (post-replace recovery)", "version", state.Version)
			os.Remove(state.TmpPath)
			clearUpdateState()
		} else {
			// Replace may have failed — rollback from .bak if exists
			if _, err := os.Stat(state.BakPath); err == nil {
				slog.Warn("gateway: rolling back after interrupted replace", "bak", state.BakPath)
				if err := os.Rename(state.BakPath, u.binPath); err != nil {
					slog.Error("gateway: rollback rename failed", "err", err, "bak", state.BakPath)
				}
				os.Chmod(u.binPath, 0755)
			}
			os.Remove(state.TmpPath)
			clearUpdateState()
		}

	case "restarting":
		// We ARE the new binary (restart succeeded, we're running)
		slog.Info("gateway: update completed successfully (post-restart)", "version", state.Version)
		os.Remove(state.TmpPath)
		clearUpdateState()

	default:
		clearUpdateState()
	}
}

// Apply performs the OTA update with persistent state at each stage.
func (u *Updater) Apply() error {
	info, err := FetchLatestK2r()
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if info.Version == u.currentVersion {
		return fmt.Errorf("already up to date")
	}

	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("k2r-linux-%s", arch)
	expectedHash, ok := info.Checksums[binaryName]
	if !ok {
		return fmt.Errorf("no binary for linux/%s", arch)
	}

	bakPath := u.binPath + ".bak"

	state := &UpdateState{
		Version:   info.Version,
		TmpPath:   fmt.Sprintf("/tmp/k2r-update-%s", info.Version),
		BakPath:   bakPath,
		StartedAt: time.Now().Unix(),
	}

	// Stage 1: Backup current binary
	state.Stage = "downloading"
	saveUpdateState(state)
	u.setProgress("downloading", 0, "")

	if err := copyFile(u.binPath, bakPath); err != nil {
		clearUpdateState()
		u.setProgress("error", 0, err.Error())
		return fmt.Errorf("backup: %w", err)
	}

	// Stage 2: Download + verify + atomic replace (DownloadAndReplace handles all)
	state.Stage = "replacing"
	saveUpdateState(state)
	u.setProgress("replacing", 0, "")

	var lastErr error
	for _, ep := range K2rReleaseEndpoints {
		downloadURL := BinaryURL(ep, info.Version)
		if lastErr = DownloadAndReplace(downloadURL, expectedHash, u.binPath); lastErr == nil {
			break
		}
		slog.Warn("gateway: download failed, trying next endpoint", "url", downloadURL, "err", lastErr)
	}
	if lastErr != nil {
		slog.Warn("gateway: all endpoints failed, rolling back", "err", lastErr)
		os.Rename(bakPath, u.binPath)
		clearUpdateState()
		u.setProgress("error", 0, lastErr.Error())
		return fmt.Errorf("download+replace: %w", lastErr)
	}

	// Stage 4: Restart
	state.Stage = "restarting"
	saveUpdateState(state)
	u.setProgress("restarting", 0, "")

	RestartService()
	// After RestartService(), this process will be killed.
	// RecoverOnStartup() in the new process handles the "restarting" state.

	u.setProgress("done", 1, "")
	return nil
}

func (u *Updater) setProgress(stage string, progress float64, errMsg string) {
	u.mu.Lock()
	u.progress = UpdateProgress{Stage: stage, Progress: progress, Error: errMsg}
	p := u.progress
	cb := u.onProgress
	u.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

// --- Update state persistence ---

func saveUpdateState(state *UpdateState) {
	data, err := json.Marshal(state)
	if err != nil {
		slog.Warn("gateway: failed to marshal update state", "err", err)
		return
	}
	if err := os.MkdirAll("/etc/k2r", 0700); err != nil {
		slog.Warn("gateway: failed to create state dir for update", "err", err)
		return
	}
	if err := os.WriteFile(updateStatePath, data, 0644); err != nil {
		slog.Warn("gateway: failed to write update state", "err", err)
	}
}

func loadUpdateState() *UpdateState {
	data, err := os.ReadFile(updateStatePath)
	if err != nil {
		return nil
	}
	var state UpdateState
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	if time.Since(time.Unix(state.StartedAt, 0)) > time.Hour {
		slog.Warn("gateway: expiring stale update state", "age", time.Since(time.Unix(state.StartedAt, 0)))
		clearUpdateState()
		return nil
	}
	return &state
}

func clearUpdateState() {
	os.Remove(updateStatePath)
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
