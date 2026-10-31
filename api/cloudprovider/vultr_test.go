package cloudprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newVultrTestProvider(t *testing.T, status int) *VultrProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer token on %s", r.URL.Path)
		}
		w.WriteHeader(status)
		switch r.URL.Path {
		case "/account/bandwidth":
			w.Write([]byte(`{"bandwidth":{
				"current_month_to_date":{"gb_in":6693,"gb_out":7080,"free_bandwidth_credits":2048,"purchased_bandwidth_credits":0,"instance_bandwidth_credits":20936},
				"current_month_projected":{"gb_in":6693,"gb_out":7080,"free_bandwidth_credits":2048,"purchased_bandwidth_credits":10,"instance_bandwidth_credits":21146}}}`))
		case "/instances":
			w.Write([]byte(`{"instances":[
				{"id":"a","label":"jp.vultr.wm02","main_ip":"107.191.60.29","v6_main_ip":"","region":"nrt","status":"active","allowed_bandwidth":2048},
				{"id":"b","label":"au-sydney.vultr.wm02","main_ip":"139.180.173.163","v6_main_ip":"","region":"syd","status":"active","allowed_bandwidth":277}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	p := NewVultrProvider("test-key")
	p.baseURL = srv.URL
	return p
}

func TestVultrListInstances_ReportsAccountPoolNotPerInstance(t *testing.T) {
	p := newVultrTestProvider(t, http.StatusOK)

	statuses, err := p.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("got %d instances, want 2", len(statuses))
	}

	wantUsed := int64(7080) << 30
	wantTotal := int64(21146+2048+10) << 30
	for _, s := range statuses {
		if s.TrafficUsedBytes != wantUsed {
			t.Errorf("%s used = %d, want pool outbound %d", s.Name, s.TrafficUsedBytes, wantUsed)
		}
		if s.TrafficTotalBytes != wantTotal {
			t.Errorf("%s total = %d, want pool allowance %d (a prorated instance must not get its own 277 GB)", s.Name, s.TrafficTotalBytes, wantTotal)
		}
		if s.TrafficResetAt.Day() != 1 || s.TrafficResetAt.Location() != time.UTC || !s.TrafficResetAt.After(time.Now()) {
			t.Errorf("%s reset = %v, want next 1st of month UTC", s.Name, s.TrafficResetAt)
		}
	}
	if statuses[1].IPAddress != "139.180.173.163" || statuses[1].Region != "syd" {
		t.Errorf("instance fields not mapped: %+v", statuses[1])
	}
}

func TestVultrGetInstanceStatus(t *testing.T) {
	p := newVultrTestProvider(t, http.StatusOK)

	status, err := p.GetInstanceStatus(context.Background(), "b")
	if err != nil {
		t.Fatalf("GetInstanceStatus: %v", err)
	}
	if status.Name != "au-sydney.vultr.wm02" {
		t.Errorf("name = %s", status.Name)
	}
	if _, err := p.GetInstanceStatus(context.Background(), "missing"); err == nil {
		t.Error("expected error for unknown instance")
	}
}

func TestVultrListInstances_APIErrorIsNotAnEmptyAccount(t *testing.T) {
	p := newVultrTestProvider(t, http.StatusUnauthorized)

	// An empty result would make the sync mark every instance deleted.
	if _, err := p.ListInstances(context.Background()); err == nil {
		t.Fatal("expected error when the API rejects the request")
	}
}

func TestNewProvider_VultrRequiresAPIKey(t *testing.T) {
	if _, err := NewProvider(ProviderConfig{Provider: ProviderVultr}); err == nil {
		t.Fatal("expected error without api_key")
	}
	p, err := NewProvider(ProviderConfig{Provider: ProviderVultr, APIKey: "k"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Name() != ProviderVultr {
		t.Errorf("name = %s", p.Name())
	}
}
