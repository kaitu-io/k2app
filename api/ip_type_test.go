package center

import "testing"

func TestNormalizeIPType(t *testing.T) {
	cases := map[string]string{
		"residential":     "residential",
		"non_residential": "non_residential",
		"unknown":         "unknown",
		"":                "unknown",
		"RESIDENTIAL":     "unknown", // 大小写不宽容,非精确即 unknown
		"garbage":         "unknown",
		"datacenter":      "unknown",
	}
	for in, want := range cases {
		if got := NormalizeIPType(in); got != want {
			t.Errorf("NormalizeIPType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProtocolDisplay(t *testing.T) {
	if got := ProtocolDisplay(TunnelProtocolK2V5); got != "k2s" {
		t.Errorf("ProtocolDisplay(k2v5) = %q, want k2s", got)
	}
	if got := ProtocolDisplay(TunnelProtocolK2V4); got != "k2v4" {
		t.Errorf("ProtocolDisplay(k2v4) = %q, want k2v4 (passthrough)", got)
	}
	if got := ProtocolDisplay(TunnelProtocolK2WSS); got != "k2wss" {
		t.Errorf("ProtocolDisplay(k2wss) = %q, want k2wss (passthrough)", got)
	}
}
