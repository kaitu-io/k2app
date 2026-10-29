//go:build linux

package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostBeaconSendsLanIPAndPort(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	err := postBeacon(context.Background(), srv.Client(), srv.URL, "192.168.8.1", 1779)
	if err != nil {
		t.Fatalf("postBeacon: %v", err)
	}
	if body["lanIP"] != "192.168.8.1" || body["port"].(float64) != 1779 {
		t.Fatalf("bad beacon body: %v", body)
	}
}
