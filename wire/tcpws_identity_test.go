package wire

import "testing"

func TestSmuxStreamConn_DeviceID(t *testing.T) {
	h := &deviceIDHolder{}
	h.set("udid-ws")
	sc := &smuxStreamConn{dev: h}
	var c any = sc
	dc, ok := c.(ConnDeviceID)
	if !ok {
		t.Fatal("smuxStreamConn must implement ConnDeviceID")
	}
	if dc.DeviceID() != "udid-ws" {
		t.Fatalf("DeviceID = %q, want udid-ws", dc.DeviceID())
	}
}

func TestSmuxStreamConn_DeviceID_NilHolder(t *testing.T) {
	sc := &smuxStreamConn{}
	if got := sc.DeviceID(); got != "" {
		t.Fatalf("nil holder DeviceID = %q, want empty", got)
	}
}
