package rule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadAndReloadNamed_UpgradesEmbedFloor is the Finding-A guard for the
// NamedBundle variant the engine actually wires (StartUpdaterNamed): a cache
// seeded from the embedded archive reads as "fresh" (mtime now + .krs present),
// so the plain 24h gate would skip the CDN download and the device would serve
// the build-time baseline for a whole TTL. DownloadAndReloadNamed must detect
// the embed provenance and FORCE a refresh that replaces the floor and clears
// the embedded marker.
func TestDownloadAndReloadNamed_UpgradesEmbedFloor(t *testing.T) {
	dir := t.TempDir()
	if err := SeedFromEmbedIfEmpty(dir); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		makeTarballHandler(t, map[string][]byte{
			"cn.krs":       {0x4b, 0x32, 0x42, 0x00},
			"overseas.krs": {0x4b, 0x32, 0x42, 0x00},
		})(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := DownloadAndReloadNamed(ctx, nil, dir, []string{srv.URL + "/"}, nil); err != nil {
		t.Fatalf("DownloadAndReloadNamed: %v", err)
	}
	if reqCount.Load() == 0 {
		t.Fatal("Named variant must also force a CDN download for an embed-seeded floor")
	}
	if wasEmbedSeeded(dir) {
		t.Error("embedded marker must be cleared after a successful CDN upgrade (Named)")
	}
}
