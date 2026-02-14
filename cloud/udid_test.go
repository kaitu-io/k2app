package cloud

import (
	"regexp"
	"testing"
)

func TestUDID_Format(t *testing.T) {
	id := UDID()
	matched, err := regexp.MatchString(`^[0-9a-f]{64}$`, id)
	if err != nil {
		t.Fatalf("regex error: %v", err)
	}
	if !matched {
		t.Errorf("UDID() = %q, want 64-char lowercase hex string", id)
	}
}

func TestUDID_Stable(t *testing.T) {
	a := UDID()
	b := UDID()
	if a != b {
		t.Errorf("UDID() not stable: first=%q, second=%q", a, b)
	}
}

func TestUDID_NotEmpty(t *testing.T) {
	id := UDID()
	if id == "" {
		t.Error("UDID() returned empty string")
	}
}
