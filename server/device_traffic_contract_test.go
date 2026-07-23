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
		Devices: []DeviceTrafficItem{{UDID: "u", Rx: 1, Tx: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"boot_id":"b1","batch_seq":2,"ts":3,"devices":[{"udid":"u","rx":1,"tx":2}]}`
	if string(b) != want {
		t.Fatalf("wire shape drifted:\n got %s\nwant %s", b, want)
	}
}
