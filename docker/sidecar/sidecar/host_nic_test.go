package sidecar

import (
	"os"
	"path/filepath"
	"testing"
)

// writeProcNetDev writes a minimal /net/dev under a temp proc root and returns
// the proc root path (e.g. tmp/proc → has tmp/proc/net/dev).
func writeProcNetDev(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "dev"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const sampleNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  100000     500    0    0    0     0          0         0   100000     500    0    0    0     0       0          0
  eth0: 1000000    2000    0    0    0     0          0         0   500000    1500    0    0    0     0       0          0
 veth9:  900000    1000    0    0    0     0          0         0   900000    1000    0    0    0     0       0          0
`

func TestReadNICBytes_SumsPhysicalSkipsVirtual(t *testing.T) {
	root := writeProcNetDev(t, sampleNetDev)
	got, err := readNICBytes(root)
	if err != nil {
		t.Fatalf("readNICBytes: %v", err)
	}
	// eth0 only: rx 1000000 + tx 500000 = 1500000. lo + veth9 excluded.
	if want := int64(1500000); got != want {
		t.Fatalf("readNICBytes = %d, want %d", got, want)
	}
}

func TestReadNICBytes_MissingFile(t *testing.T) {
	if _, err := readNICBytes(t.TempDir()); err == nil {
		t.Fatal("expected error for missing net/dev")
	}
}
