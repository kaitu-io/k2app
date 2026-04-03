package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

func cmdConnect(args []string) int {
	fs := flag.NewFlagSet("k2 connect", flag.ContinueOnError)
	mode := fs.String("mode", "proxy", "traffic mode: proxy or tun")
	listen := fs.String("listen", "127.0.0.1:1080", "proxy listen address (proxy mode only)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Connect to a k2 server in foreground mode.

Usage: k2 connect <URL> [flags]

  No daemon, no service installation, no configuration files.
  Default mode is proxy (SOCKS5+HTTP on 127.0.0.1:1080). Ctrl+C to stop.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  k2 connect k2v5://user:token@host:443?ech=...
  k2 connect k2v5://... --mode tun             # VPN mode (needs sudo)
  k2 connect k2v5://... --listen 0.0.0.0:1080  # custom proxy address
`)
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	wireURL := fs.Arg(0)
	if !strings.Contains(wireURL, "://") {
		out.fail("INVALID_URL", fmt.Sprintf("expected k2v5:// URL, got %q", wireURL), "")
		return 1
	}

	cfg := config.ClientFromURL(wireURL)
	cfg.Mode = *mode
	if *mode == "proxy" {
		cfg.Proxy.Listen = *listen
	}

	if err := config.ValidateClient(cfg); err != nil {
		out.fail("INVALID_CONFIG", err.Error(), "")
		return 1
	}

	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2")
	if err != nil {
		slog.Error("logging setup failed", "err", err)
		return 2
	}
	defer cleanup()

	if cfg.CacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.CacheDir = filepath.Join(home, ".cache", "k2rule")
		}
	}

	eng := engine.New()
	eng.SetEventHandler(&connectHandler{})

	ecfg := engine.Config{
		Client:         cfg,
		FileDescriptor: -1,
	}

	out.event(map[string]any{"event": "connecting", "server": cfg.Server, "mode": cfg.Mode})

	if err := eng.Start(ecfg); err != nil {
		out.fail("CONNECT_FAILED", err.Error(), "")
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()

	eng.Stop()
	out.event(map[string]any{"event": "disconnected", "reason": "signal"})
	return 0
}

type connectHandler struct{}

func (h *connectHandler) OnStatus(status engine.Status) {
	event := map[string]any{"event": status.State}
	if status.Error != nil {
		event["error"] = status.Error.Message
		event["code"] = status.Error.Code
	}
	out.event(event)
}

func (h *connectHandler) OnStats(txBytes, rxBytes int64) {
	// Stats not emitted in connect mode (too noisy).
}
