//go:build windows

package provider

import "testing"

func TestNtohs_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want uint16
	}{
		{0x000031D4, 54321}, // 0xD431 in NBO
		{0x00005000, 80},    // 0x0050 in NBO
		{0x0000BB01, 443},   // 0x01BB in NBO
		{0x00000000, 0},
		{0x0000FFFF, 0xFFFF},
	}
	for _, c := range cases {
		got := ntohs(c.in)
		if got != c.want {
			t.Errorf("ntohs(0x%08X) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestUint32ToIPv4_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want [4]byte
	}{
		{0x0501A8C0, [4]byte{192, 168, 1, 5}},
		{0x0100007F, [4]byte{127, 0, 0, 1}},
		{0x00000000, [4]byte{0, 0, 0, 0}},
		{0x010101D5, [4]byte{213, 1, 1, 1}}, // 213.1.1.1 ← 0xD5010101 NBO
	}
	for _, c := range cases {
		got := uint32ToIPv4(c.in)
		if got != c.want {
			t.Errorf("uint32ToIPv4(0x%08X) = %v, want %v", c.in, got, c.want)
		}
	}
}
