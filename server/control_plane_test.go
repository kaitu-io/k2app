package server

import (
	"testing"

	"github.com/kaitu-io/k2/config"
)

func TestValidateControlPlaneRoutes(t *testing.T) {
	cases := []struct {
		name    string
		routes  map[string]string
		wantErr bool
	}{
		{"empty map ok", map[string]string{}, false},
		{"valid public origin", map[string]string{"k2.52j.me": "k2.52j.me:443"}, false},
		{"empty sni", map[string]string{"": "k2.52j.me:443"}, true},
		{"empty origin", map[string]string{"k2.52j.me": ""}, true},
		{"missing port", map[string]string{"k2.52j.me": "k2.52j.me"}, true},
		{"loopback origin rejected", map[string]string{"k2.52j.me": "127.0.0.1:443"}, true},
		{"private origin rejected", map[string]string{"k2.52j.me": "10.0.0.5:443"}, true},
		{"unspecified origin rejected", map[string]string{"k2.52j.me": "0.0.0.0:443"}, true},
		{"link-local unicast rejected", map[string]string{"k2.52j.me": "169.254.0.1:443"}, true},
		{"link-local multicast rejected", map[string]string{"k2.52j.me": "224.0.0.1:443"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateControlPlaneRoutes(c.routes)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateControlPlaneRoutes(%v) err=%v, wantErr=%v", c.routes, err, c.wantErr)
			}
		})
	}
}

func TestMatchControlPlaneRoute(t *testing.T) {
	s := &Server{cfg: &config.ServerConfig{
		ControlPlaneRoutes: map[string]string{"k2.52j.me": "k2.52j.me:443"},
	}}
	if origin, ok := s.matchControlPlaneRoute("k2.52j.me"); !ok || origin != "k2.52j.me:443" {
		t.Fatalf("exact match: got (%q,%v), want (k2.52j.me:443,true)", origin, ok)
	}
	if _, ok := s.matchControlPlaneRoute("evil.example.com"); ok {
		t.Fatal("non-listed inner SNI must not match")
	}
	if _, ok := s.matchControlPlaneRoute(""); ok {
		t.Fatal("empty inner SNI must not match")
	}
	empty := &Server{cfg: &config.ServerConfig{}}
	if _, ok := empty.matchControlPlaneRoute("k2.52j.me"); ok {
		t.Fatal("nil ControlPlaneRoutes must not match")
	}
}
