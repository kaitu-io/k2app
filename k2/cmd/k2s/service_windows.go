//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
)

const k2sServiceName = "k2s"

// installService installs k2s as a Windows service and starts it.
func installService() error {
	exe, err := resolveExecutable()
	if err != nil {
		return err
	}

	err = exec.Command("sc", "create", k2sServiceName,
		"binPath=", fmt.Sprintf(`"%s" run --foreground`, exe),
		"start=", "auto",
		"DisplayName=", "k2s network tunnel server",
	).Run()
	if err != nil {
		return fmt.Errorf("sc create: %w", err)
	}

	if err := exec.Command("sc", "start", k2sServiceName).Run(); err != nil {
		return fmt.Errorf("sc start: %w", err)
	}

	return nil
}

// uninstallService stops and deletes the k2s Windows service.
func uninstallService() error {
	// Stop the service first (ignore error if not running).
	_ = exec.Command("sc", "stop", k2sServiceName).Run()

	// Delete the service.
	if err := exec.Command("sc", "delete", k2sServiceName).Run(); err != nil {
		return fmt.Errorf("sc delete: %w", err)
	}

	return nil
}

// isServiceInstalled reports whether the k2s service is registered with SCM.
func isServiceInstalled() bool {
	err := exec.Command("sc", "query", k2sServiceName).Run()
	return err == nil
}

// isServiceRunning reports whether the k2s service is in RUNNING state.
func isServiceRunning() bool {
	out, err := exec.Command("sc", "query", k2sServiceName).Output()
	return err == nil && bytes.Contains(out, []byte("RUNNING"))
}

// startService starts the k2s Windows service.
func startService() error {
	return exec.Command("sc", "start", k2sServiceName).Run()
}

// stopRunningService stops the k2s service if running.
func stopRunningService() {
	_ = exec.Command("sc", "stop", k2sServiceName).Run()
}

// installServiceWithConfig installs k2s as a Windows service with -c config flag and starts it.
func installServiceWithConfig(configPath string) error {
	exe, err := resolveExecutable()
	if err != nil {
		return err
	}

	// Stop and delete existing service if any
	_ = exec.Command("sc", "stop", k2sServiceName).Run()
	_ = exec.Command("sc", "delete", k2sServiceName).Run()

	err = exec.Command("sc", "create", k2sServiceName,
		"binPath=", fmt.Sprintf(`"%s" run --foreground -c "%s"`, exe, configPath),
		"start=", "auto",
		"DisplayName=", "k2s network tunnel server",
	).Run()
	if err != nil {
		return fmt.Errorf("sc create: %w", err)
	}

	if err := exec.Command("sc", "start", k2sServiceName).Run(); err != nil {
		return fmt.Errorf("sc start: %w", err)
	}

	return nil
}
