//go:build linux

package gateway

import "testing"

func TestSlotLocalRoundTrip(t *testing.T) {
	withTempStateDir(t)

	s := loadSlotLocal()
	if len(s.TakenOver) != 0 {
		t.Fatalf("fresh state not empty: %+v", s)
	}
	s.TakenOver[3] = true
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s2 := loadSlotLocal()
	if !s2.TakenOver[3] {
		t.Fatal("takeover flag lost")
	}
}
