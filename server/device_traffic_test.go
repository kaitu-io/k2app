package server

import (
	"sync"
	"testing"
)

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
