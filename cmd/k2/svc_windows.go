//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
	"github.com/kaitu-io/k2/daemon/wintun"
	"golang.org/x/sys/windows/svc"
)

// kaituService implements svc.Handler for Windows Service Control Manager.
type kaituService struct {
	configPath string
}

// Execute is called by the Windows SCM. It starts the daemon, reports
// SERVICE_RUNNING, then waits for a stop request.
func (s *kaituService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	// Set up logging.
	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2")
	if err != nil {
		slog.Error("Failed to set up default logging", "err", err)
		return false, 1
	}
	defer cleanup()

	cfg, cfgPathUsed, err := loadOrDefaultConfig(s.configPath)
	if err != nil {
		slog.Error("Failed to load config", "err", err)
		return false, 1
	}
	if cfgPathUsed != "" {
		slog.Info("Using config", "path", cfgPathUsed)
	}

	cleanup2, err := config.SetupLogging(cfg.Log, "k2")
	if err != nil {
		slog.Error("Failed to set up logging from config", "err", err)
		return false, 1
	}
	defer cleanup2()

	if err := config.ValidateClient(cfg); err != nil {
		slog.Error("Config validation failed", "err", err)
		return false, 1
	}

	// Skip PID file in service mode — SCM tracks the service process.

	if _, err := wintun.EnsureExtracted(); err != nil {
		slog.Info("wintun", "err", err)
	}
	daemon.ApplyResourceLimits()

	go checkVersionAsync()

	d := daemon.New()
	d.Version = version
	d.Commit = commit
	if cfgPathUsed != "" {
		d.ConfigPath = cfgPathUsed
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start daemon in background goroutine.
	errCh := make(chan error, 1)
	go func() {
		defer daemon.HandlePanic()
		errCh <- d.Run(ctx, cfg.Listen)
	}()

	// Report running — SCM considers the service started.
	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	slog.Info("Service reported RUNNING to SCM")

	// Wait for SCM stop/shutdown or daemon error.
	for {
		select {
		case cr := <-r:
			switch cr.Cmd {
			case svc.Interrogate:
				changes <- cr.CurrentStatus
			case svc.Stop, svc.Shutdown:
				slog.Info("SCM requested stop")
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-errCh
				return false, 0
			}
		case err := <-errCh:
			if err != nil && err != context.Canceled {
				slog.Error("Daemon error", "err", err)
				return false, 1
			}
			return false, 0
		}
	}
}

// startDaemon dispatches between Windows service mode and foreground daemon.
func startDaemon(configPath string) {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		slog.Warn("Cannot detect service mode, assuming interactive", "err", err)
	}

	if isSvc {
		slog.Info("Running as Windows service")
		if err := svc.Run("kaitu", &kaituService{configPath: configPath}); err != nil {
			slog.Error("Windows service failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Interactive (foreground) mode.
	runDaemon(configPath)
}
