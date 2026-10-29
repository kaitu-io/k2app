package rule

import "testing"

func TestParsePortRange(t *testing.T) {
	cases := []struct {
		in      string
		lo, hi  uint16
		wantErr bool
	}{
		{in: "443", lo: 443, hi: 443},
		{in: "27000-28000", lo: 27000, hi: 28000},
		{in: "1-65535", lo: 1, hi: 65535},
		{in: " 80 ", lo: 80, hi: 80},
		{in: "", wantErr: true},
		{in: "0", wantErr: true},
		{in: "65536", wantErr: true},
		{in: "100-50", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "1-2-3", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParsePortRange(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParsePortRange(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsePortRange(%q): %v", c.in, err)
			continue
		}
		if got.Lo != c.lo || got.Hi != c.hi {
			t.Errorf("ParsePortRange(%q) = %d-%d, want %d-%d", c.in, got.Lo, got.Hi, c.lo, c.hi)
		}
	}
}

func TestPortRangeContains(t *testing.T) {
	r := PortRange{Lo: 27000, Hi: 28000}
	for _, p := range []uint16{27000, 27500, 28000} {
		if !r.Contains(p) {
			t.Errorf("Contains(%d) = false, want true", p)
		}
	}
	for _, p := range []uint16{0, 26999, 28001} {
		if r.Contains(p) {
			t.Errorf("Contains(%d) = true, want false", p)
		}
	}
}

// TestPortRangeContains_ZeroNeverMatchesWideRange pins the port-0 guard on its
// own. The range above starts at 27000, so 0 is already excluded by Lo — delete
// the guard and that assertion still passes. Only a range whose Lo is 0 lets a
// missing guard through, and ConnMeta.Port uses 0 for "port unknown": without
// the guard such a connection would match every rule spanning the full range.
func TestPortRangeContains_ZeroNeverMatchesWideRange(t *testing.T) {
	wide := PortRange{Lo: 0, Hi: 65535}
	if wide.Contains(0) {
		t.Error("Contains(0) = true on a 0-65535 range, want false (0 means unknown port)")
	}
	if !wide.Contains(1) || !wide.Contains(65535) {
		t.Error("wide range must still contain real ports 1 and 65535")
	}
}
