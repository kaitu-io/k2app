//go:build linux

package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/kaitu-io/k2/config"
)

func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v (body=%s)", err, w.Body.String())
	}
	return resp
}

func TestHandleSetSlotSSID(t *testing.T) {
	withTempStateDir(t)

	var ran [][]string
	prevRun := runSlotWifiCmdsFn
	runSlotWifiCmdsFn = func(cmds [][]string) error { ran = append(ran, cmds...); return nil }
	t.Cleanup(func() { runSlotWifiCmdsFn = prevRun })

	g := &Gateway{}
	g.mu.Lock()
	g.slotBindings = []config.SlotBinding{{Slot: 3, Country: "ae", Index: 1, TunnelIndex: 0}}
	g.mu.Unlock()

	w := httptest.NewRecorder()
	g.handleSetSlotSSID(w, map[string]any{"slot": float64(3), "ssid": "My Studio"})
	resp := decodeResponse(t, w)
	if resp.Code != 0 {
		t.Fatalf("code=%d msg=%s", resp.Code, resp.Message)
	}
	if len(ran) == 0 || !reflect.DeepEqual(ran[0], []string{"uci", "set", "wireless.slot3.ssid=My Studio"}) {
		t.Fatalf("cmds: %v", ran)
	}
	if !loadSlotLocal().TakenOver[3] {
		t.Fatal("takeover flag not persisted")
	}

	// 未绑定 slot → 错误
	w = httptest.NewRecorder()
	g.handleSetSlotSSID(w, map[string]any{"slot": float64(7), "ssid": "x"})
	if resp := decodeResponse(t, w); resp.Code != 1 {
		t.Fatalf("unbound slot want Code=1, got %d", resp.Code)
	}

	// 非法 ssid → 错误
	w = httptest.NewRecorder()
	g.handleSetSlotSSID(w, map[string]any{"slot": float64(3), "ssid": "bad\n"})
	if resp := decodeResponse(t, w); resp.Code != 1 {
		t.Fatalf("invalid ssid want Code=1, got %d", resp.Code)
	}
}

func TestHandleSetSlotPassword_NotMultiSlot(t *testing.T) {
	withTempStateDir(t)
	g := &Gateway{}
	w := httptest.NewRecorder()
	g.handleSetSlotPassword(w, map[string]any{"slot": float64(1), "password": "goodpass1"})
	if resp := decodeResponse(t, w); resp.Code != 1 {
		t.Fatalf("consumer mode want Code=1, got %d", resp.Code)
	}
}
