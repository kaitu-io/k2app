//go:build !mobile

package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/electricbubble/gadb"
)

var cliAndroidEndpoints = []string{
	"https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android",
	"https://d0.all7.cc/kaitu/android",
}

func cmdAndroidInstall(args []string) {
	fmt.Println()
	fmt.Println("📱 Kaitu Android 安装助手")
	fmt.Println("========================")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Developer options
	fmt.Println("步骤 1/4: 开启开发者选项")
	fmt.Println("  在手机上: 设置 → 关于手机 → 连续点击7次\"版本号\"")
	fmt.Print("  完成后按 Enter 继续...")
	reader.ReadString('\n')
	fmt.Println()

	// Step 2: USB debugging
	fmt.Println("步骤 2/4: 开启 USB 调试")
	fmt.Println("  在手机上: 设置 → 开发者选项 → 开启\"USB调试\"")
	fmt.Print("  完成后按 Enter 继续...")
	reader.ReadString('\n')
	fmt.Println()

	// Step 3: USB connect + detect device
	fmt.Println("步骤 3/4: USB 连接")
	fmt.Print("  正在准备工具...")

	adbPath, err := cliPrepareAdb()
	if err != nil {
		fmt.Printf(" 失败\n  ❌ %s\n", err)
		os.Exit(1)
	}
	fmt.Println(" 完成")
	fmt.Println("  请用数据线连接手机到电脑...")
	fmt.Println("  ⚠️  请在手机上点击\"允许USB调试\"")
	fmt.Println()

	var device gadb.Device
	for {
		fmt.Print("  检测中...")
		client, err := gadb.NewClientWith("localhost", cliAdbPort)
		if err != nil {
			fmt.Printf(" 未检测到设备 (adb server: %s)\n", err)
			fmt.Print("  按 Enter 重试...")
			reader.ReadString('\n')
			continue
		}

		devices, err := client.DeviceList()
		if err != nil || len(devices) == 0 {
			fmt.Println(" 未检测到设备")
			fmt.Print("  按 Enter 重试...")
			reader.ReadString('\n')
			continue
		}

		device = devices[0]
		model := device.Serial()
		if m, err := device.RunShellCommand("getprop", "ro.product.model"); err == nil {
			m = strings.TrimSpace(m)
			if m != "" {
				model = m
			}
		}

		state, _ := device.State()
		if state != gadb.StateOnline {
			fmt.Printf(" 发现设备: %s (未授权 - 请在手机上允许USB调试)\n", model)
			fmt.Print("  按 Enter 重试...")
			reader.ReadString('\n')
			continue
		}

		fmt.Printf(" 发现设备: %s ✓\n", model)
		break
	}
	fmt.Println()

	// Step 4: Install
	fmt.Println("步骤 4/4: 安装 Kaitu")

	// Determine APK source
	var apkURL string
	if len(args) > 0 {
		apkURL = args[0]
	}

	var downloadURL, expectedHash string
	var expectedSize int64
	var apkVersion string

	if apkURL == "" {
		fmt.Print("  获取最新版本...")
		manifest, err := cliFetchLatestAPK()
		if err != nil {
			fmt.Printf(" 失败\n  ❌ %s\n", err)
			os.Exit(1)
		}
		if strings.HasPrefix(manifest.URL, "http") {
			downloadURL = manifest.URL
		} else {
			downloadURL = cliAndroidEndpoints[0] + "/" + manifest.URL
		}
		expectedHash = manifest.Hash
		expectedSize = manifest.Size
		apkVersion = manifest.Version
		fmt.Printf(" v%s\n", apkVersion)
	} else {
		downloadURL = apkURL
		fmt.Printf("  使用指定 APK: %s\n", apkURL)
	}

	// Download
	fmt.Print("  下载中...")
	apkPath, err := cliDownloadAPK(downloadURL, expectedHash, expectedSize)
	if err != nil {
		fmt.Printf(" 失败\n  ❌ %s\n", err)
		os.Exit(1)
	}
	defer os.Remove(apkPath)
	fmt.Println(" 完成")

	// Push to device
	fmt.Print("  推送到手机...")
	remotePath := "/data/local/tmp/k2-install.apk"
	apkFile, err := os.Open(apkPath)
	if err != nil {
		fmt.Printf(" 失败\n  ❌ %s\n", err)
		os.Exit(1)
	}

	if err := device.Push(apkFile, remotePath, time.Now()); err != nil {
		apkFile.Close()
		fmt.Printf(" 失败\n  ❌ push: %s\n", err)
		os.Exit(1)
	}
	apkFile.Close()
	fmt.Println(" 完成")

	// Install on device
	fmt.Print("  安装中...")
	output, err := device.RunShellCommand("pm", "install", "-r", "-d", remotePath)
	if err != nil {
		fmt.Printf(" 失败\n  ❌ pm install: %s\n", err)
		os.Exit(1)
	}

	output = strings.TrimSpace(output)
	if !strings.Contains(output, "Success") {
		fmt.Printf(" 失败\n  ❌ %s\n", output)
		os.Exit(1)
	}
	fmt.Println(" 完成")

	// Cleanup remote file
	device.RunShellCommand("rm", remotePath)

	fmt.Println()
	if apkVersion != "" {
		fmt.Printf("  ✅ 安装完成！Kaitu v%s 已安装到手机。\n", apkVersion)
	} else {
		fmt.Println("  ✅ 安装完成！打开手机即可使用。")
	}
	fmt.Println()

	_ = adbPath
}

// cliAdbPort is the port used by the CLI for adb connections.
// Set by cliPrepareAdb after detecting or starting a server.
var cliAdbPort = 5037

// cliPrepareAdb ensures adb is available. Reuses existing server on 5037 first.
func cliPrepareAdb() (string, error) {
	// 1. Try reusing existing adb server on default port
	client, err := gadb.NewClientWith("localhost", 5037)
	if err == nil {
		if _, err := client.ServerVersion(); err == nil {
			cliAdbPort = 5037
			return "", nil // no binary path needed, server already running
		}
	}

	// 2. Download our own adb binary
	manifest, err := cliFetchToolsManifest()
	if err != nil {
		return "", fmt.Errorf("fetch tools manifest: %w", err)
	}

	osKey := runtime.GOOS
	entry, ok := manifest.ADB.Files[osKey]
	if !ok {
		return "", fmt.Errorf("no adb binary for %s", osKey)
	}

	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = os.TempDir()
	}
	adbDir := filepath.Join(cacheBase, "k2", "tools", "adb-"+manifest.ADB.Version)
	adbBin := "adb"
	if runtime.GOOS == "windows" {
		adbBin = "adb.exe"
	}
	adbPath := filepath.Join(adbDir, adbBin)

	// Check cache
	if _, err := os.Stat(adbPath); err != nil {
		// Download zip
		zipPath := filepath.Join(os.TempDir(), "k2-adb-"+entry.URL)
		if err := cliDownloadFile("tools/"+entry.URL, zipPath, entry.Hash); err != nil {
			return "", fmt.Errorf("download adb: %w", err)
		}
		defer os.Remove(zipPath)

		if err := cliExtractAdb(zipPath, adbDir); err != nil {
			return "", fmt.Errorf("extract adb: %w", err)
		}

		if runtime.GOOS == "darwin" {
			exec.Command("xattr", "-rd", "com.apple.quarantine", adbDir).Run()
		}
		if runtime.GOOS != "windows" {
			os.Chmod(adbPath, 0755)
		}
	}

	// Start server on default port
	cmd := exec.Command(adbPath, "start-server")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("start adb server: %w", err)
	}
	cliAdbPort = 5037

	return adbPath, nil
}

type cliToolsManifest struct {
	ADB struct {
		Version string                       `json:"version"`
		Files   map[string]cliToolsFileEntry `json:"files"`
	} `json:"adb"`
}

type cliToolsFileEntry struct {
	URL  string `json:"url"`
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type cliAPKManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
}

func cliFetchToolsManifest() (*cliToolsManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, endpoint := range cliAndroidEndpoints {
		resp, err := client.Get(endpoint + "/tools/tools.json")
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		var m cliToolsManifest
		if err := json.Unmarshal(data, &m); err != nil {
			lastErr = err
			continue
		}
		return &m, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

func cliFetchLatestAPK() (*cliAPKManifest, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, endpoint := range cliAndroidEndpoints {
		resp, err := client.Get(endpoint + "/latest.json")
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		var m cliAPKManifest
		if err := json.Unmarshal(data, &m); err != nil {
			lastErr = err
			continue
		}
		return &m, nil
	}
	return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}

func cliDownloadFile(path, destPath, expectedHash string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	var lastErr error
	for _, endpoint := range cliAndroidEndpoints {
		resp, err := client.Get(endpoint + "/" + path)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}

		os.MkdirAll(filepath.Dir(destPath), 0755)
		f, err := os.Create(destPath)
		if err != nil {
			resp.Body.Close()
			return err
		}

		hasher := sha256.New()
		_, err = io.Copy(io.MultiWriter(f, hasher), resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			os.Remove(destPath)
			lastErr = err
			continue
		}

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

func cliDownloadAPK(url, expectedHash string, expectedSize int64) (string, error) {
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		cacheBase = os.TempDir()
	}
	destDir := filepath.Join(cacheBase, "k2", "android")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		// Fall back to temp dir if cache dir is owned by root
		destDir = filepath.Join(os.TempDir(), "k2-android")
		os.MkdirAll(destDir, 0755)
	}

	parts := strings.Split(url, "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		filename = "install.apk"
	}
	destPath := filepath.Join(destDir, filename)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", err
	}

	hasher := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, hasher), resp.Body)
	f.Close()
	if err != nil {
		os.Remove(destPath)
		return "", err
	}

	if expectedHash != "" {
		actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
		if actual != expectedHash {
			os.Remove(destPath)
			return "", fmt.Errorf("APK hash mismatch")
		}
	}
	return destPath, nil
}

func cliExtractAdb(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(destDir, 0755)

	needed := map[string]bool{"adb": true}
	if runtime.GOOS == "windows" {
		needed = map[string]bool{
			"adb.exe":          true,
			"AdbWinApi.dll":    true,
			"AdbWinUsbApi.dll": true,
		}
	}

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if !needed[base] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(destDir, base))
		if err != nil {
			rc.Close()
			return err
		}
		io.Copy(out, rc)
		rc.Close()
		out.Close()
	}
	return nil
}
