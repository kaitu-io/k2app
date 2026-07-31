package server

import (
	"sync"
	"testing"
)

// fakeDeviceConn implements wire.ConnDeviceID only.
type fakeDeviceConn struct{ udid string }

func (f fakeDeviceConn) DeviceID() string { return f.udid }

// fakeSessionConn implements wire.ConnSessionID only — isolates the
// ConnSessionID branch in connDeviceKey (server/device_traffic.go) from the
// ConnDeviceID branch, so a regression that drops only the session branch
// still reddens this test even though DeviceID-based tests would stay green.
type fakeSessionConn struct{ session string }

func (f fakeSessionConn) SessionID() string { return f.session }

// fakeDeviceSessionConn implements both interfaces.
type fakeDeviceSessionConn struct {
	udid    string
	session string
}

func (f fakeDeviceSessionConn) DeviceID() string  { return f.udid }
func (f fakeDeviceSessionConn) SessionID() string { return f.session }

func TestConnDeviceKey_SessionBranch(t *testing.T) {
	got := connDeviceKey(fakeSessionConn{session: "sess-abc123"})
	want := DeviceKey{UDID: "", Session: "sess-abc123"}
	if got != want {
		t.Fatalf("connDeviceKey = %+v, want %+v", got, want)
	}
}

func TestConnDeviceKey_DeviceBranch(t *testing.T) {
	got := connDeviceKey(fakeDeviceConn{udid: "dev-1"})
	want := DeviceKey{UDID: "dev-1", Session: ""}
	if got != want {
		t.Fatalf("connDeviceKey = %+v, want %+v", got, want)
	}
}

func TestConnDeviceKey_BothInterfaces(t *testing.T) {
	got := connDeviceKey(fakeDeviceSessionConn{udid: "dev-1", session: "sess-1"})
	want := DeviceKey{UDID: "dev-1", Session: "sess-1"}
	if got != want {
		t.Fatalf("connDeviceKey = %+v, want %+v", got, want)
	}
}

func TestConnDeviceKey_NoInterfaces_ZeroKey(t *testing.T) {
	got := connDeviceKey(struct{}{})
	if got != (DeviceKey{}) {
		t.Fatalf("connDeviceKey = %+v, want zero DeviceKey", got)
	}
}

func TestDeviceTraffic_AddTake(t *testing.T) {
	var dt deviceTraffic
	dt.Add(DeviceKey{UDID: "a"}, 10, 20)
	dt.Add(DeviceKey{UDID: "a"}, 1, 2)
	dt.Add(DeviceKey{UDID: "b"}, 5, 0)
	dt.Add(DeviceKey{}, 7, 7) // unattributed bucket is legal

	got := dt.Take()
	if got[DeviceKey{UDID: "a"}] != (DeviceDelta{Rx: 11, Tx: 22}) {
		t.Fatalf("a = %+v", got[DeviceKey{UDID: "a"}])
	}
	if got[DeviceKey{UDID: "b"}] != (DeviceDelta{Rx: 5, Tx: 0}) {
		t.Fatalf("b = %+v", got[DeviceKey{UDID: "b"}])
	}
	if got[DeviceKey{}] != (DeviceDelta{Rx: 7, Tx: 7}) {
		t.Fatalf("empty bucket = %+v", got[DeviceKey{}])
	}
	// Take must drain.
	if second := dt.Take(); len(second) != 0 {
		t.Fatalf("second Take should be empty, got %v", second)
	}
}

func TestDeviceTraffic_ZeroDeltaIgnored(t *testing.T) {
	var dt deviceTraffic
	dt.Add(DeviceKey{UDID: "a"}, 0, 0)
	if got := dt.Take(); len(got) != 0 {
		t.Fatalf("zero delta should not create an entry, got %v", got)
	}
}

func TestDeviceTraffic_ConcurrentAdd(t *testing.T) {
	var dt deviceTraffic
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				dt.Add(DeviceKey{UDID: "x"}, 1, 1)
			}
		}()
	}
	wg.Wait()
	got := dt.Take()
	if got[DeviceKey{UDID: "x"}] != (DeviceDelta{Rx: 5000, Tx: 5000}) {
		t.Fatalf("x = %+v, want {5000 5000}", got[DeviceKey{UDID: "x"}])
	}
}
