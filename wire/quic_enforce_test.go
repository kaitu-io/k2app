package wire

import (
	"log/slog"
	"testing"
	"time"
)

// TestProxyAuthorized covers the QUIC proxy-stream admission gate — the actual
// hole-closing decision. Focus is the enforce/permissive split and the timing
// safety around a proxy stream that races ahead of the metadata stream.
func TestProxyAuthorized(t *testing.T) {
	newCtx := func() *connContext {
		return &connContext{authDone: make(chan struct{})}
	}
	log := slog.Default()

	t.Run("permissive allows even unauthenticated", func(t *testing.T) {
		s := &QUICServer{enforce: false, authWait: 10 * time.Millisecond}
		ctx := newCtx() // authOK=false, authDone open (old client, no metadata)
		if !s.proxyAuthorized(ctx, log) {
			t.Error("permissive must never block — attribution only")
		}
	})

	t.Run("enforce allows authenticated", func(t *testing.T) {
		s := &QUICServer{enforce: true, authWait: 10 * time.Millisecond}
		ctx := newCtx()
		ctx.authOK.Store(true)
		close(ctx.authDone)
		if !s.proxyAuthorized(ctx, log) {
			t.Error("authenticated connection must pass under enforce")
		}
	})

	t.Run("enforce rejects unauthenticated after metadata processed", func(t *testing.T) {
		s := &QUICServer{enforce: true, authWait: 10 * time.Millisecond}
		ctx := newCtx()
		close(ctx.authDone) // metadata seen, validation failed (authOK stays false)
		if s.proxyAuthorized(ctx, log) {
			t.Error("failed-auth connection must be rejected under enforce")
		}
	})

	t.Run("enforce rejects no-metadata client after timeout", func(t *testing.T) {
		s := &QUICServer{enforce: true, authWait: 20 * time.Millisecond}
		ctx := newCtx() // authDone never closes — legacy client that never sends metadata
		start := time.Now()
		if s.proxyAuthorized(ctx, log) {
			t.Error("no-metadata client must be rejected under enforce")
		}
		if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
			t.Errorf("should have waited ~authWait for late metadata, waited %v", elapsed)
		}
	})

	t.Run("enforce tolerates proxy-before-metadata race", func(t *testing.T) {
		s := &QUICServer{enforce: true, authWait: 500 * time.Millisecond}
		ctx := newCtx()
		// Simulate the metadata stream landing shortly AFTER the proxy stream —
		// a legitimate client whose streams the server accepted out of order.
		go func() {
			time.Sleep(20 * time.Millisecond)
			ctx.authOK.Store(true)
			close(ctx.authDone)
		}()
		if !s.proxyAuthorized(ctx, log) {
			t.Error("a valid client whose proxy stream raced ahead must wait then pass")
		}
	})
}
