package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type fakeSource struct{ batches []map[DeviceKey]DeviceDelta }

func (f *fakeSource) TakeDeviceTraffic() map[DeviceKey]DeviceDelta {
	if len(f.batches) == 0 {
		return map[DeviceKey]DeviceDelta{}
	}
	b := f.batches[0]
	f.batches = f.batches[1:]
	return b
}

func newTestReporter(src deviceTrafficSource, url string) *deviceTrafficReporter {
	return newDeviceTrafficReporter(src, url, "1.2.3.4", "secret")
}

func TestDeviceReporter_HappyPath(t *testing.T) {
	var got DeviceTrafficRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"code":0,"message":"","data":{}}`))
	}))
	defer srv.Close()

	src := &fakeSource{batches: []map[DeviceKey]DeviceDelta{{{UDID: "u1"}: {Rx: 10, Tx: 20}}}}
	r := newTestReporter(src, srv.URL)
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if r.pending != nil {
		t.Fatal("pending must clear on ack")
	}
	if got.BootID == "" || got.BatchSeq != 1 || len(got.Devices) != 1 {
		t.Fatalf("bad request: %+v", got)
	}
	if got.Devices[0].UDID != "u1" || got.Devices[0].Rx != 10 || got.Devices[0].Tx != 20 {
		t.Fatalf("bad device item: %+v", got.Devices[0])
	}
}

func TestDeviceReporter_EmptySnapshotSkipsPost(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()
	r := newTestReporter(&fakeSource{}, srv.URL)
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if posts.Load() != 0 {
		t.Fatal("empty snapshot must not POST")
	}
}

// 核心：失败保批次 → 重发同 boot_id/seq/内容；新增量不并入未确认批次。
func TestDeviceReporter_RetryResendsImmutableBatch(t *testing.T) {
	var calls []DeviceTrafficRequest
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeviceTrafficRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls = append(calls, req)
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(`{"code":0,"data":{}}`))
	}))
	defer srv.Close()

	src := &fakeSource{batches: []map[DeviceKey]DeviceDelta{
		{{UDID: "u1"}: {Rx: 10}},
		{{UDID: "u2"}: {Rx: 99}}, // 第二批，必须等第一批确认后才被取走
	}}
	r := newTestReporter(src, srv.URL)

	if err := r.runOnce(context.Background()); err == nil {
		t.Fatal("first cycle should fail")
	}
	fail = false
	if err := r.runOnce(context.Background()); err != nil { // 重发第一批
		t.Fatalf("retry: %v", err)
	}
	if err := r.runOnce(context.Background()); err != nil { // 取第二批
		t.Fatalf("third: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	// 第 1、2 次是同一不可变批次
	if calls[0].BatchSeq != calls[1].BatchSeq || calls[0].BootID != calls[1].BootID {
		t.Fatalf("retry must reuse boot/seq: %+v vs %+v", calls[0], calls[1])
	}
	if calls[1].Devices[0].UDID != "u1" || calls[1].Devices[0].Rx != 10 {
		t.Fatalf("retry payload mutated: %+v", calls[1].Devices)
	}
	// 第 3 次是新批次
	if calls[2].BatchSeq != calls[0].BatchSeq+1 {
		t.Fatalf("next batch seq = %d, want %d", calls[2].BatchSeq, calls[0].BatchSeq+1)
	}
	if calls[2].Devices[0].UDID != "u2" {
		t.Fatalf("next batch payload: %+v", calls[2].Devices)
	}
}
