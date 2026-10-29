//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/kaitu-io/k2/config"
)

const serviceName = "kaitu"

// legacyServiceNames are old service names from previous versions that should
// be cleaned up during install to avoid conflicts.
var legacyServiceNames = []string{"k2", "kaitu-service"}

func installService() error {
	// Diagnostic trail for the desktop shell: every step lands in
	// k2-service-install.log (uploaded with feedback logs). The install
	// itself must never fail because of logging — flush is best-effort.
	il := &installLog{}
	defer il.flush()
	il.logf("k2 service install starting: version=%s elevated=%v", version, config.IsRoot())

	exe, err := os.Executable()
	if err != nil {
		il.logf("FAIL find executable: %v", err)
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		il.logf("FAIL resolve executable path: %v", err)
		return fmt.Errorf("resolve executable path: %w", err)
	}
	il.logf("exe=%s", exe)

	m, err := mgr.Connect()
	if err != nil {
		il.logf("FAIL connect to SCM: %v", err)
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	// Clean up legacy service names from previous versions.
	for _, name := range legacyServiceNames {
		il.logf("cleanup legacy %q: %s", name, cleanupService(m, name))
	}

	// Idempotent: if service already exists, stop and delete it first.
	il.logf("cleanup existing %q: %s", serviceName, cleanupService(m, serviceName))

	// Create the service. Retry briefly because SCM may still be purging
	// the old service record after Delete (ERROR_SERVICE_MARKED_DELETE).
	var s *mgr.Service
	for attempt := 0; attempt < 10; attempt++ {
		s, err = m.CreateService(serviceName, exe, mgr.Config{
			DisplayName: "Kaitu Network Service",
			StartType:   mgr.StartAutomatic,
		}, "run")
		if err == nil {
			il.logf("create service: OK (attempt %d)", attempt+1)
			break
		}
		il.logf("create service attempt %d: %v", attempt+1, err)
		if attempt < 9 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		il.logf("FAIL create service after retries: %v", err)
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Configure recovery: restart on failure.
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
	}, 3600) // reset failure count after 1 hour
	if err != nil {
		il.logf("warn: set recovery actions: %v", err)
		slog.Warn("Failed to set recovery actions", "err", err)
	}

	// Enable recovery on non-crash failures (e.g., os.Exit(1)).
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		il.logf("warn: enable non-crash failure recovery: %v", err)
		slog.Warn("Failed to enable non-crash failure recovery", "err", err)
	}

	// Start the service.
	if err := s.Start(); err != nil {
		il.logf("FAIL start service: %v", err)
		return fmt.Errorf("start service: %w", err)
	}

	il.logf("service started OK")
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
// Used for idempotent install and legacy service cleanup. Returns a short
// human-readable summary for the install diagnostic log.
func cleanupService(m *mgr.Mgr, name string) string {
	s, err := m.OpenService(name)
	if err != nil {
		return "not present"
	}
	defer s.Close()

	// Best-effort stop.
	s.Control(svc.Stop)
	// Wait briefly for the service to stop.
	stopped := false
	for i := 0; i < 10; i++ {
		status, qerr := s.Query()
		if qerr != nil {
			break
		}
		if status.State == svc.Stopped {
			stopped = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Delete the service.
	if err := s.Delete(); err != nil {
		slog.Warn("Failed to delete service", "name", name, "err", err)
		return fmt.Sprintf("stopped=%v delete FAILED: %v", stopped, err)
	}
	return fmt.Sprintf("stopped=%v deleted", stopped)
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
