// server/device_traffic_contract_test.go
package server

import (
	"encoding/json"
	"testing"
)

// Pins the wire contract with Center (api/slave_api_device_traffic.go).
// A tag change here MUST be mirrored there — this test is the tripwire.
func TestDeviceTrafficRequest_WireShape(t *testing.T) {
	b, err := json.Marshal(DeviceTrafficRequest{
		BootID: "b1", BatchSeq: 2, Ts: 3,
		Devices: []DeviceTrafficItem{
			{UDID: "u1", SessionID: "s1", Rx: 1, Tx: 2},
			{UDID: "u2", Rx: 3, Tx: 4}, // legacy/unattributed: no session_id (omitempty)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"boot_id":"b1","batch_seq":2,"ts":3,"devices":[{"udid":"u1","session_id":"s1","rx":1,"tx":2},{"udid":"u2","rx":3,"tx":4}]}`
	if string(b) != want {
		t.Fatalf("wire shape drifted:\n got %s\nwant %s", b, want)
	}
}

// spec §6.6: accounting buckets are keyed by (udid, session) — same-session
// deltas merge, different sessions of the same udid stay separate, and the
// zero key (unattributed) survives as its own bucket.
func TestDeviceTraffic_PerSessionBuckets(t *testing.T) {
	var d deviceTraffic
	d.Add(DeviceKey{UDID: "u1", Session: "s1"}, 10, 0)
	d.Add(DeviceKey{UDID: "u1", Session: "s2"}, 0, 20)
	d.Add(DeviceKey{UDID: "u1", Session: "s1"}, 5, 0)
	d.Add(DeviceKey{UDID: "", Session: ""}, 1, 1) // unattributed bucket survives
	got := d.Take()
	if len(got) != 3 {
		t.Fatalf("buckets = %d, want 3 (two sessions + unattributed)", len(got))
	}
	if got[DeviceKey{UDID: "u1", Session: "s1"}].Rx != 15 {
		t.Fatal("same-session deltas not merged")
	}
}
