package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	ossignal "os/signal"
	"path/filepath"
	"syscall"

	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/server"
	"github.com/kaitu-io/k2/wire"
)

func cmdPeerRun(args []string) {
	// 1. Load config.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: home dir: %v\n", err)
		os.Exit(1)
	}
	cfgPath := filepath.Join(home, ".k2p", "config.json")

	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read config: %v\nRun 'k2 peer setup' first.\n", err)
		os.Exit(1)
	}
	var cfg k2pConfig
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: parse config: %v\n", err)
		os.Exit(1)
	}

	// 2. Load TLS cert pair.
	certPair, err := wire.LoadCertPair(cfg.CertDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: load cert: %v\n", err)
		os.Exit(1)
	}

	// 3. Detect networks.
	networks, dns := k2p.DetectPrivateNetworks()
	slog.Info("k2p: detected networks", "cidrs", networks, "dns", dns)

	// 4. Build k2p config and start listening.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k2pCfg := k2p.Config{
		HomeUDID:     cfg.UDID,
		SignalServer: cfg.SignalBase,
		RegToken:     cfg.RegToken,
		SecretHash:   cfg.SecretHash,
		TLSCert:      &certPair.EC,
		Networks:     networks,
		DNS:          dns,
	}

	listenerCh, err := k2p.Listen(ctx, k2pCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: listen: %v\n", err)
		os.Exit(1)
	}

	slog.Info("k2p: home peer running", "udid", cfg.UDID, "networks", networks)

	// 5. Create proxy handler.
	handler := server.NewProxyHandler()
	handler.AllowPrivateIP = true

	// 6. Accept listeners from channel and serve.
	safego.Go(func() {
		for ln := range listenerCh {
			serveListener(ctx, ln, handler)
		}
	})

	// 7. Wait for signal.
	sigCh := make(chan os.Signal, 1)
	ossignal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("k2p: shutting down", "signal", sig.String())
	cancel()
	handler.Close()
}

// serveListener accepts TCP and UDP connections from a single wire.Listener.
func serveListener(ctx context.Context, ln wire.Listener, handler *server.ProxyHandler) {
	// TCP accept loop.
	safego.Go(func() {
		for {
			conn, addr, err := ln.AcceptTCP(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("k2p: accept TCP", "err", err)
				return
			}
			safego.Go(func() {
				handler.HandleTCP(conn, addr)
			})
		}
	})

	// UDP serve.
	safego.Go(func() {
		err := ln.ServeUDP(ctx, wire.UDPHandlerFunc(func(conn net.PacketConn, addr string) {
			handler.HandleUDP(conn, addr)
		}))
		if err != nil && ctx.Err() == nil {
			slog.Warn("k2p: serve UDP", "err", err)
		}
	})
}
