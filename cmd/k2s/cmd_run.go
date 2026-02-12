package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/server"
)

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	serverCfg := fs.String("s", "", "server config file")
	install := fs.Bool("install", false, "install as system service")
	fs.Parse(args)

	switch {
	case *install:
		fmt.Println("Service installation not yet implemented for k2s")
		os.Exit(1)
	case *serverCfg != "":
		runServer(*serverCfg)
	default:
		fmt.Fprintln(os.Stderr, "Usage: k2s run -s <server.yaml>")
		os.Exit(1)
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
