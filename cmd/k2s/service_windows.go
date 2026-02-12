//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const k2sServiceName = "k2s"

func installK2sService() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
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

func isK2sServiceInstalled() bool {
	err := exec.Command("sc", "query", k2sServiceName).Run()
	return err == nil
}

func isK2sServiceRunning() bool {
	out, err := exec.Command("sc", "query", k2sServiceName).Output()
	return err == nil && bytes.Contains(out, []byte("RUNNING"))
}

func startK2sService() error {
	return exec.Command("sc", "start", k2sServiceName).Run()
}
