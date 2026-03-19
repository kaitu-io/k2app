package daemon

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"
)

// adbDefaultPort is the standard adb server port. We try to reuse an existing
// server on this port first (e.g. Android Studio's) to avoid USB device conflicts.
// Only if no server is running do we start our own.
const adbDefaultPort = 5037

// adbFallbackPort is used when the default port is occupied by a non-ADB process.
const adbFallbackPort = 5038

// androidEndpoints is the CDN fallback list for Android-related downloads.
// Both tools/tools.json and latest.json are fetched from these endpoints in order.
var androidEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android",
	"https://d0.all7.cc/kaitu/android",
}

// toolsManifest is the schema for tools/tools.json on CDN.
type toolsManifest struct {
	ADB struct {
		Version string                    `json:"version"`
		Files   map[string]toolsFileEntry `json:"files"` // key: "darwin" or "windows"
	} `json:"adb"`
}

type toolsFileEntry struct {
	URL  string `json:"url"`  // relative filename (e.g. "platform-tools-darwin.zip")
	Hash string `json:"hash"` // "sha256:..." hex digest
	Size int64  `json:"size"`
}

// progressReader wraps an io.Reader to report read progress via callback.
type progressReader struct {
	reader   io.Reader
	total    int64
	current  int64
	callback func(current, total int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.current += int64(n)
	if pr.callback != nil {
		pr.callback(pr.current, pr.total)
	}
	return
}

// adbCacheDir returns the directory for cached adb tools.
func adbCacheDir(version string) string {
	return filepath.Join(cacheDir(), "tools", "adb-"+version)
}

// apkCacheDir returns the directory for cached APK files.
func apkCacheDir() string {
	return filepath.Join(cacheDir(), "android")
}

// prepareAdb ensures an adb server is available for device operations.
// Strategy: reuse existing server on port 5037 first (avoids USB conflicts
// with Android Studio), then download our own and start if none exists.
func (h *AndroidHelper) prepareAdb() error {
	h.mu.Lock()
	if h.adbPort != 0 {
		// Already prepared
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	// 1. Try reusing an existing adb server on the default port
	if h.tryReuseExistingServer(adbDefaultPort) {
		return nil
	}

	// 2. No existing server — download our own adb binary and start it
	adbPath, err := h.downloadAdbBinary()
	if err != nil {
		return err
	}

	return h.startOwnAdbServer(adbPath)
}

// tryReuseExistingServer checks if a working adb server exists on the given port.
func (h *AndroidHelper) tryReuseExistingServer(port int) bool {
	client, err := gadb.NewClientWith("localhost", port)
	if err != nil {
		return false
	}
	// Verify it's a real adb server by requesting the server version
	if _, err := client.ServerVersion(); err != nil {
		return false
	}

	h.mu.Lock()
	h.adbPort = port
	h.weStartedServer = false
	h.mu.Unlock()

	slog.Info("helper: reusing existing adb server", "port", port)
	return true
}

// downloadAdbBinary fetches the adb binary from CDN if not already cached.
func (h *AndroidHelper) downloadAdbBinary() (string, error) {
	manifest, err := fetchToolsManifest()
	if err != nil {
		return "", fmt.Errorf("fetch tools manifest: %w", err)
	}

	osKey := runtime.GOOS
	entry, ok := manifest.ADB.Files[osKey]
	if !ok {
		return "", fmt.Errorf("no adb binary available for %s", osKey)
	}

	adbDir := adbCacheDir(manifest.ADB.Version)
	adbBin := "adb"
	if runtime.GOOS == "windows" {
		adbBin = "adb.exe"
	}
	adbPath := filepath.Join(adbDir, adbBin)

	// Check if already cached
	if _, err := os.Stat(adbPath); err == nil {
		slog.Info("helper: adb already cached", "path", adbPath)
		return adbPath, nil
	}

	// Download zip
	slog.Info("helper: downloading adb tools", "version", manifest.ADB.Version, "os", osKey)
	zipPath := filepath.Join(os.TempDir(), "k2-adb-"+entry.URL)
	if err := downloadWithFallback("tools/"+entry.URL, zipPath, entry.Hash, entry.Size); err != nil {
		return "", fmt.Errorf("download adb tools: %w", err)
	}
	defer os.Remove(zipPath)

	if err := extractAdbFromZip(zipPath, adbDir); err != nil {
		return "", fmt.Errorf("extract adb: %w", err)
	}

	removeQuarantine(adbDir)

	if runtime.GOOS != "windows" {
		os.Chmod(adbPath, 0755)
	}

	return adbPath, nil
}

// startOwnAdbServer starts our own adb server on the default port (5037).
// Falls back to 5038 if 5037 is occupied by a non-ADB process.
func (h *AndroidHelper) startOwnAdbServer(adbPath string) error {
	// Try default port first (no -P flag = 5037)
	port := adbDefaultPort
	cmd := exec.Command(adbPath, "start-server")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		slog.Warn("helper: adb start-server on default port failed, trying fallback", "error", err)
		// Fallback to 5038
		port = adbFallbackPort
		portStr := strconv.Itoa(port)
		exec.Command(adbPath, "-P", portStr, "kill-server").Run()
		if err := exec.Command(adbPath, "-P", portStr, "start-server").Run(); err != nil {
			return fmt.Errorf("start adb server: %w", err)
		}
	}

	h.mu.Lock()
	h.adbPath = adbPath
	h.adbPort = port
	h.weStartedServer = true
	h.mu.Unlock()

	slog.Info("helper: adb server started", "path", adbPath, "port", port)
	return nil
}

// stopAdbServer kills the adb server during daemon shutdown, but only if we started it.
func (h *AndroidHelper) stopAdbServer() {
	h.mu.Lock()
	adbPath := h.adbPath
	port := h.adbPort
	weStarted := h.weStartedServer
	h.mu.Unlock()

	if !weStarted || adbPath == "" {
		return
	}

	args := []string{"kill-server"}
	if port != adbDefaultPort {
		args = []string{"-P", strconv.Itoa(port), "kill-server"}
	}
	cmd := exec.Command(adbPath, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	slog.Info("helper: adb server stopped", "port", port)
}

// fetchToolsManifest fetches tools/tools.json from CDN with fallback.
func fetchToolsManifest() (*toolsManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error

	for _, endpoint := range androidEndpoints {
		url := endpoint + "/tools/tools.json"
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}

		var manifest toolsManifest
		err = decodeJSONBody(resp.Body, &manifest)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

// downloadWithFallback downloads a file from CDN with endpoint fallback and hash verification.
func downloadWithFallback(path, destPath, expectedHash string, expectedSize int64) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	var lastErr error

	for _, endpoint := range androidEndpoints {
		url := endpoint + "/" + path
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}

		// Download to temp file
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			resp.Body.Close()
			return fmt.Errorf("create dir: %w", err)
		}
		f, err := os.Create(destPath)
		if err != nil {
			resp.Body.Close()
			return fmt.Errorf("create file: %w", err)
		}

		hasher := sha256.New()
		writer := io.MultiWriter(f, hasher)
		_, err = io.Copy(writer, resp.Body)
		resp.Body.Close()
		f.Close()

		if err != nil {
			os.Remove(destPath)
			lastErr = fmt.Errorf("download: %w", err)
			continue
		}

		// Verify hash
		if expectedHash != "" {
			actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
			if actual != expectedHash {
				os.Remove(destPath)
				lastErr = fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actual)
				continue
			}
		}

		return nil
	}
	return fmt.Errorf("all endpoints failed: %w", lastErr)
}

// extractAdbFromZip extracts all top-level files from platform-tools/ in the zip.
// Google bundles matched dependencies (DLLs, etc.), so we extract everything
// rather than maintaining a fragile whitelist of individual files.
func extractAdbFromZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	const prefix = "platform-tools/"
	extracted := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		// Only top-level files in platform-tools/, skip subdirectories
		rel := strings.TrimPrefix(f.Name, prefix)
		if strings.Contains(rel, "/") {
			continue
		}

		base := filepath.Base(f.Name)
		destPath := filepath.Join(destDir, base)
		out, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", base, err)
		}

		rc, err := f.Open()
		if err != nil {
			out.Close()
			return fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}

		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return fmt.Errorf("extract %s: %w", base, err)
		}
		extracted++
		slog.Debug("helper: extracted", "file", base, "dest", destPath)
	}

	if extracted == 0 {
		return fmt.Errorf("no adb files found in zip")
	}
	return nil
}

// removeQuarantine removes OS quarantine flags from downloaded binaries.
func removeQuarantine(dir string) {
	switch runtime.GOOS {
	case "darwin":
		// macOS: xattr -d com.apple.quarantine
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			exec.Command("xattr", "-d", "com.apple.quarantine", path).Run()
			return nil
		})
	case "windows":
		// Windows: delete Zone.Identifier ADS
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			os.Remove(path + ":Zone.Identifier")
			return nil
		})
	}
}

// decodeJSONBody decodes JSON from an io.ReadCloser with a 1MB limit.
func decodeJSONBody(r io.Reader, v any) error {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return err
	}
	return jsonUnmarshal(data, v)
}

// jsonUnmarshal wraps json.Unmarshal for consistency.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// apkManifest is the schema for android/latest.json on CDN.
type apkManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`  // relative filename (e.g. "0.5.0/Kaitu-0.5.0.apk")
	Hash    string `json:"hash"` // "sha256:..." hex digest
	Size    int64  `json:"size"`
}

// fetchLatestAPK fetches android/latest.json from CDN with fallback.
func fetchLatestAPK() (*apkManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error

	for _, endpoint := range androidEndpoints {
		url := endpoint + "/latest.json"
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}

		var manifest apkManifest
		err = decodeJSONBody(resp.Body, &manifest)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return &manifest, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

// downloadAPK downloads an APK file with progress tracking.
// destDir specifies where to save the file. Returns the local file path.
func (h *AndroidHelper) downloadAPK(apkURL, expectedHash string, expectedSize int64, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create apk cache dir: %w", err)
	}

	// Use filename from URL
	parts := strings.Split(apkURL, "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		filename = "install.apk"
	}
	destPath := filepath.Join(destDir, filename)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(apkURL)
	if err != nil {
		return "", fmt.Errorf("download APK: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download APK: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("create APK file: %w", err)
	}

	total := resp.ContentLength
	if total <= 0 && expectedSize > 0 {
		total = expectedSize
	}

	hasher := sha256.New()
	pr := &progressReader{
		reader: resp.Body,
		total:  total,
		callback: func(current, t int64) {
			if t > 0 {
				pct := int(current * 100 / t)
				h.setProgress(pct)
			}
		},
	}

	_, err = io.Copy(io.MultiWriter(f, hasher), pr)
	f.Close()
	if err != nil {
		os.Remove(destPath)
		return "", fmt.Errorf("download APK: %w", err)
	}

	// Verify hash if provided
	if expectedHash != "" {
		actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actual != expectedHash {
			os.Remove(destPath)
			return "", fmt.Errorf("APK hash mismatch: expected %s, got %s", expectedHash, actual)
		}
	}

	return destPath, nil
}

// doInstall runs the full async APK installation pipeline.
// Phases: prepare_adb → downloading → pushing → installing → done | error
func (h *AndroidHelper) doInstall(serial, apkURL string) {
	// Phase: prepare_adb
	slog.Info("helper: doInstall starting", "serial", serial, "url", apkURL)
	h.setPhase("prepare_adb", 0)

	if err := h.prepareAdb(); err != nil {
		h.setError(fmt.Sprintf("prepare adb: %s", err))
		return
	}

	// Resolve APK source
	var downloadURL, expectedHash string
	var expectedSize int64

	if apkURL == "" {
		// Kaitu default: fetch latest.json
		manifest, err := fetchLatestAPK()
		if err != nil {
			h.setError(fmt.Sprintf("fetch latest.json: %s", err))
			return
		}
		// Resolve URL: use as-is if absolute, otherwise prepend CDN endpoint
		if strings.HasPrefix(manifest.URL, "http") {
			downloadURL = manifest.URL
		} else {
			downloadURL = androidEndpoints[0] + "/" + manifest.URL
		}
		expectedHash = manifest.Hash
		expectedSize = manifest.Size

		h.mu.Lock()
		h.state.Version = manifest.Version
		h.mu.Unlock()

		slog.Info("helper: Kaitu APK resolved", "version", manifest.Version, "url", downloadURL)
	} else {
		downloadURL = apkURL
	}

	// Phase: downloading
	h.setPhase("downloading", 0)
	apkPath, err := h.downloadAPK(downloadURL, expectedHash, expectedSize, apkCacheDir())
	if err != nil {
		h.setError(fmt.Sprintf("download APK: %s", err))
		return
	}
	defer os.Remove(apkPath)

	// Find target device
	h.mu.Lock()
	port := h.adbPort
	h.mu.Unlock()
	client, err := gadb.NewClientWith("localhost", port)
	if err != nil {
		h.setError(fmt.Sprintf("connect to adb: %s", err))
		return
	}

	device, err := resolveDevice(client, serial)
	if err != nil {
		h.setError(err.Error())
		return
	}
	slog.Info("helper: target device", "serial", device.Serial())

	// Phase: pushing
	h.setPhase("pushing", 0)
	remotePath := "/data/local/tmp/k2-install.apk"

	apkFile, err := os.Open(apkPath)
	if err != nil {
		h.setError(fmt.Sprintf("open APK: %s", err))
		return
	}

	apkStat, _ := apkFile.Stat()
	pr := &progressReader{
		reader: apkFile,
		total:  apkStat.Size(),
		callback: func(current, total int64) {
			if total > 0 {
				h.setProgress(int(current * 100 / total))
			}
		},
	}

	if err := device.Push(pr, remotePath, time.Now()); err != nil {
		apkFile.Close()
		h.setError(fmt.Sprintf("push APK to device: %s", err))
		return
	}
	apkFile.Close()

	// Phase: installing
	h.setPhase("installing", 0)
	output, err := device.RunShellCommand("pm", "install", "-r", "-d", remotePath)
	if err != nil {
		h.setError(fmt.Sprintf("pm install: %s", err))
		return
	}

	// Check pm install output
	output = strings.TrimSpace(output)
	slog.Info("helper: pm install output", "output", output)
	if !strings.Contains(output, "Success") {
		h.setError(fmt.Sprintf("pm install failed: %s", output))
		return
	}

	// Cleanup remote APK
	device.RunShellCommand("rm", remotePath)

	// Done
	h.setPhase("done", 100)
	slog.Info("helper: install complete")
}

// resolveDevice finds the target device by serial, or auto-selects if only one is connected.
func resolveDevice(client gadb.Client, serial string) (gadb.Device, error) {
	devices, err := client.DeviceList()
	if err != nil {
		return gadb.Device{}, fmt.Errorf("list devices: %s", err)
	}
	if len(devices) == 0 {
		return gadb.Device{}, fmt.Errorf("no devices connected")
	}

	if serial == "" {
		if len(devices) == 1 {
			return devices[0], nil
		}
		serials := make([]string, len(devices))
		for i, d := range devices {
			serials[i] = d.Serial()
		}
		return gadb.Device{}, fmt.Errorf("multiple devices connected (%s), specify serial", strings.Join(serials, ", "))
	}

	for _, d := range devices {
		if d.Serial() == serial {
			return d, nil
		}
	}
	return gadb.Device{}, fmt.Errorf("device %s not found", serial)
}

// triggerDriverScan runs pnputil /scan-devices on Windows.
// Returns true on first call (scan triggered), false on subsequent calls.
// pnputil /scan-devices returns in ~20ms; actual driver download is async via Windows Update.
func (h *AndroidHelper) triggerDriverScan() bool {
	h.mu.Lock()
	if h.driverScanDone {
		h.mu.Unlock()
		return false
	}
	h.driverScanDone = true
	h.mu.Unlock()

	slog.Info("helper: triggering Windows driver scan via pnputil")
	cmd := exec.Command("pnputil", "/scan-devices")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		slog.Warn("helper: pnputil scan failed", "error", err)
		return false
	}
	slog.Info("helper: pnputil scan completed")
	return true
}
