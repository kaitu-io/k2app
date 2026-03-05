//go:build windows

package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "kaitu"

// legacyServiceNames are old service names from previous versions that should
// be cleaned up during install to avoid conflicts.
var legacyServiceNames = []string{"k2", "kaitu-service"}

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	// Clean up legacy service names from previous versions.
	for _, name := range legacyServiceNames {
		cleanupService(m, name)
	}

	// Idempotent: if service already exists, stop and delete it first.
	cleanupService(m, serviceName)

	// Create the service. Retry briefly because SCM may still be purging
	// the old service record after Delete (ERROR_SERVICE_MARKED_DELETE).
	var s *mgr.Service
	for attempt := 0; attempt < 10; attempt++ {
		s, err = m.CreateService(serviceName, exe, mgr.Config{
			DisplayName: "Kaitu Network Service",
			StartType:   mgr.StartAutomatic,
		})
		if err == nil {
			break
		}
		if attempt < 9 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Configure recovery: restart on failure.
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	}, 60) // reset failure count after 60 seconds
	if err != nil {
		slog.Warn("Failed to set recovery actions", "err", err)
	}

	// Enable recovery on non-crash failures (e.g., os.Exit(1)).
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		slog.Warn("Failed to enable non-crash failure recovery", "err", err)
	}

	// Start the service.
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	// Also clean up any legacy service names.
	for _, name := range legacyServiceNames {
		cleanupService(m, name)
	}

	return stopAndDelete(m, serviceName)
}

func isServiceInstalled() bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

// cleanupService stops and deletes a service by name, ignoring errors.
// Used for idempotent install and legacy service cleanup.
func cleanupService(m *mgr.Mgr, name string) {
	s, err := m.OpenService(name)
	if err != nil {
		return // service doesn't exist
	}
	defer s.Close()

	// Best-effort stop.
	s.Control(svc.Stop)
	// Wait briefly for the service to stop.
	for i := 0; i < 10; i++ {
		status, err := s.Query()
		if err != nil || status.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Delete the service.
	if err := s.Delete(); err != nil {
		slog.Warn("Failed to delete service", "name", name, "err", err)
	}
}

// stopAndDelete stops and deletes a service, returning errors.
func stopAndDelete(m *mgr.Mgr, name string) error {
	s, err := m.OpenService(name)
	if err != nil {
		return nil // service doesn't exist, nothing to do
	}
	defer s.Close()

	// Stop the service (ignore error if already stopped).
	s.Control(svc.Stop)
	// Wait for it to stop.
	for i := 0; i < 20; i++ {
		status, err := s.Query()
		if err != nil || status.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service %s: %w", name, err)
	}
	return nil
}
