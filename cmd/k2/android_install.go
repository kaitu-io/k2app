//go:build !mobile

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/kaitu-io/k2/daemon"
)

func cmdAndroidInstall(args []string) {
	fs := flag.NewFlagSet("k2 android-install", flag.ExitOnError)
	serial := fs.String("serial", "", "device serial (auto-select if only one)")
	apkURL := fs.String("url", "", "APK download URL (default: latest Kaitu)")
	timeout := fs.Int("timeout", 300, "max seconds to wait for install")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Install APK to a USB-connected Android device.

Usage: k2 android-install [flags]

  Requires the k2 daemon to be running (k2 run).
  Detects connected devices, downloads APK, and installs via ADB.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  k2 android-install                              # install latest Kaitu APK
  k2 android-install --serial ABC123              # target specific device
  k2 android-install --url https://example.com/app.apk --json
`)
	}
	fs.Parse(args)

	// 1. Detect devices via daemon.
	client := helperClient()

	out.printf("Detecting devices...\n")
	detectResp, err := helperDo(client, "adb-detect", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		os.Exit(2)
	}
	if detectResp.Code != 0 {
		out.fail("ADB_DETECT_FAILED", detectResp.Message, "")
		os.Exit(2)
	}

	detectData := marshalData(detectResp.Data)
	adbReady, _ := detectData["adb_ready"].(bool)
	if !adbReady {
		out.fail("ADB_NOT_READY", "ADB not available", "connect an Android device with USB debugging enabled")
		os.Exit(2)
	}

	devices, _ := detectData["devices"].([]any)
	if len(devices) == 0 {
		out.fail("NO_DEVICE", "no Android device found", "connect a device with USB debugging enabled, then tap 'Allow' on the device")
		os.Exit(2)
	}

	// Auto-select serial if not specified.
	targetSerial := *serial
	if targetSerial == "" {
		if len(devices) > 1 {
			out.fail("MULTIPLE_DEVICES", fmt.Sprintf("%d devices connected, specify --serial", len(devices)), "")
			if out.json {
				// Print device list as JSON for scripting.
				out.ok(detectData)
			} else {
				for _, d := range devices {
					if dm, ok := d.(map[string]any); ok {
						fmt.Fprintf(os.Stderr, "  %s (%s) — %s\n", dm["serial"], dm["model"], dm["state"])
					}
				}
			}
			os.Exit(1)
		}
		if dm, ok := devices[0].(map[string]any); ok {
			targetSerial, _ = dm["serial"].(string)
		}
	}

	out.printf("Target device: %s\n", targetSerial)

	// 2. Start install via daemon (async).
	installParams := map[string]any{"serial": targetSerial}
	if *apkURL != "" {
		installParams["url"] = *apkURL
	}

	installResp, err := helperDo(client, "adb-install", installParams)
	if err != nil {
		out.fail("INSTALL_REQUEST_FAILED", err.Error(), "")
		os.Exit(2)
	}
	if installResp.Code != 0 {
		out.fail("INSTALL_FAILED", installResp.Message, "")
		os.Exit(2)
	}

	out.printf("Installing...\n")

	// 3. Poll status until done or timeout.
	deadline := time.Now().Add(time.Duration(*timeout) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)

		statusResp, err := helperDo(client, "adb-status", nil)
		if err != nil {
			continue
		}

		data := marshalData(statusResp.Data)
		phase, _ := data["phase"].(string)
		progress, _ := data["progress"].(float64)
		version, _ := data["version"].(string)
		errMsg, _ := data["error"].(string)

		switch phase {
		case "done":
			msg := "Install complete"
			if version != "" {
				msg = fmt.Sprintf("Install complete (Kaitu v%s)", version)
			}
			out.printf("%s\n", msg)
			out.ok(data)
			return

		case "error":
			out.fail("INSTALL_FAILED", errMsg, "")
			os.Exit(2)

		default:
			out.printf("  %s %.0f%%\n", phase, progress)
		}
	}

	out.fail("INSTALL_TIMEOUT", fmt.Sprintf("install did not complete within %ds", *timeout), "check 'k2 android-install --json' for status")
	os.Exit(2)
}

// helperClient returns an HTTP client for the daemon helper API.
func helperClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// helperDo sends a request to the daemon /api/helper endpoint.
func helperDo(client *http.Client, action string, params map[string]any) (*daemon.Response, error) {
	req := daemon.Request{Action: action, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := client.Post(
		fmt.Sprintf("http://%s/api/helper", daemon.DefaultAddr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result daemon.Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// marshalData round-trips Data through JSON to get a consistent map[string]any.
func marshalData(data any) map[string]any {
	b, _ := json.Marshal(data)
	var m map[string]any
	json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}
