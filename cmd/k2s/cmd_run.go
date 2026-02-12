package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/server"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	serverCfg := fs.String("s", "", "server config file")
	foreground := fs.Bool("foreground", false, "run in foreground (used by service manager)")
	fs.Parse(args)

	switch {
	case *serverCfg != "":
		runServer(*serverCfg)
	case *foreground:
		runServerDefault()
	default:
		runSmart()
	}
}

// runSmart is the default `k2s run` mode. It ensures k2s is installed as a
// system service, starts it if needed, waits for the connect URL, and prints it.
func runSmart() {
	certDir := config.DefaultServerConfig().CertDir
	urlPath := filepath.Join(certDir, "connect-url.txt")

	// Already running? Just print the URL.
	if isK2sServiceRunning() {
		if url := readConnectURL(urlPath); url != "" {
			fmt.Println(url)
			return
		}
		fmt.Println("k2s is running but connect URL not yet available. Waiting...")
		if url := waitForConnectURL(urlPath, 30*time.Second); url != "" {
			fmt.Println(url)
			return
		}
		fmt.Fprintln(os.Stderr, "Timed out waiting for connect URL.")
		fmt.Fprintf(os.Stderr, "Check logs or run: k2s run --foreground\n")
		os.Exit(1)
	}

	// Not installed → install.
	if !isK2sServiceInstalled() {
		fmt.Println("Installing k2s service...")
		if err := installK2sService(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to install service: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try running with sudo, or use: k2s run --foreground\n")
			os.Exit(1)
		}
		fmt.Println("Service installed.")
	} else {
		// Installed but not running → start.
		fmt.Println("Starting k2s service...")
		if err := startK2sService(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start service: %v\n", err)
			fmt.Fprintf(os.Stderr, "Try running with sudo, or use: k2s run --foreground\n")
			os.Exit(1)
		}
	}

	// Wait for connect URL to appear.
	fmt.Println("Waiting for server to start...")
	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		fmt.Fprintln(os.Stderr, "Timed out waiting for server to start.")
		fmt.Fprintf(os.Stderr, "Check logs or run: k2s run --foreground\n")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("k2s is running. Share this URL with clients:")
	fmt.Println()
	fmt.Println("  " + url)
	fmt.Println()
}

// readConnectURL reads the connect URL from the file, returning empty string if not found.
func readConnectURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// waitForConnectURL polls for the connect-url.txt file to appear.
func waitForConnectURL(path string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if url := readConnectURL(path); url != "" {
			return url
		}
		time.Sleep(500 * time.Millisecond)
	}
	return ""
}

func runServerDefault() {
	cfg := config.DefaultServerConfig()

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}
}

func runServer(cfgPath string) {
	cfg, err := config.LoadServer(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		cancel()
	}()

	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Server error: %v", err)
	}
}
