package wire

import "testing"

func TestDeviceIDHolder_SetGet(t *testing.T) {
	h := &deviceIDHolder{}
	if got := h.get(); got != "" {
		t.Fatalf("empty holder should return %q, got %q", "", got)
	}
	h.set("dev-abc")
	if got := h.get(); got != "dev-abc" {
		t.Fatalf("get = %q, want dev-abc", got)
	}
}

func TestQuicStreamConn_DeviceID(t *testing.T) {
	h := &deviceIDHolder{}
	h.set("udid-1")
	sc := &quicStreamConn{dev: h}
	var c any = sc
	dc, ok := c.(ConnDeviceID)
	if !ok {
		t.Fatal("quicStreamConn must implement ConnDeviceID")
	}
	if dc.DeviceID() != "udid-1" {
		t.Fatalf("DeviceID = %q, want udid-1", dc.DeviceID())
	}
}

func TestQuicStreamConn_DeviceID_NilHolder(t *testing.T) {
	sc := &quicStreamConn{} // no holder wired (defensive)
	if got := sc.DeviceID(); got != "" {
		t.Fatalf("nil holder DeviceID = %q, want empty", got)
	}
}
