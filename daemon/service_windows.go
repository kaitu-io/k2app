//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const k2ServiceName = "k2"

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	err = exec.Command("sc", "create", k2ServiceName,
		"binPath=", fmt.Sprintf(`"%s" run`, exe),
		"start=", "auto",
		"DisplayName=", "k2 network tunnel daemon",
	).Run()
	if err != nil {
		return fmt.Errorf("sc create: %w", err)
	}

	if err := exec.Command("sc", "start", k2ServiceName).Run(); err != nil {
		return fmt.Errorf("sc start: %w", err)
	}

	return nil
}

func uninstallService() error {
	exec.Command("sc", "stop", k2ServiceName).Run()
	exec.Command("sc", "delete", k2ServiceName).Run()
	return nil
}

func isServiceInstalled() bool {
	err := exec.Command("sc", "query", k2ServiceName).Run()
	return err == nil
}
