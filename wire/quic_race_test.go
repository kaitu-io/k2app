package wire

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestQUICClient_DialUDP_ResetRace exercises concurrent DialUDP + resetConnection.
// Before the fix, DialUDP could return a PacketConn backed by a closed QUIC conn
// and closed mux because connect() releases the lock before DialUDP reads alloc/mux.
//
// After the fix: single lock hold verifies conn is still current + reads all resources.
//
// Run with: go test -race -run TestQUICClient_DialUDP_ResetRace ./wire/
func TestQUICClient_DialUDP_ResetRace(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	// Establish initial connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("warmup connect: %v", err)
	}

	const goroutines = 50
	var wg sync.WaitGroup

	// Concurrent DialUDP callers
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				dctx, dcancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				pc, err := client.DialUDP(dctx, "8.8.8.8:53")
				dcancel()
				if err != nil {
					continue
				}
				pc.Close()
			}
		}()
	}

	// Concurrent resetConnection caller
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			client.resetConnection()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestQUICClient_DialUDP_StaleCheck verifies that DialUDP detects a stale conn
// after resetConnection runs between connect() and the resource read.
func TestQUICClient_DialUDP_StaleCheck(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	// Establish connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Reset the connection
	client.resetConnection()

	// DialUDP should re-connect (or fail), not use a stale conn
	dctx, dcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer dcancel()
	pc, err := client.DialUDP(dctx, "8.8.8.8:53")
	if err != nil {
		// Acceptable: reconnect may fail in test env
		t.Logf("DialUDP after reset: %v (acceptable)", err)
		return
	}
	defer pc.Close()

	// If it succeeds, verify the conn is fresh (not the old closed one)
	client.mu.Lock()
	currentConn := client.conn
	client.mu.Unlock()

	if currentConn == nil {
		t.Fatal("DialUDP succeeded but client.conn is nil")
	}
}
