package server

import "testing"

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
