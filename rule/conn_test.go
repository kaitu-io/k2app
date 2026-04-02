package rule

import "testing"

func TestConnMeta_Zero(t *testing.T) {
	var m ConnMeta
	if m.ProcessName != "" {
		t.Error("zero ConnMeta should have empty ProcessName")
	}
	if m.Network != "" {
		t.Error("zero ConnMeta should have empty Network")
	}
	if m.Port != 0 {
		t.Error("zero ConnMeta should have zero Port")
	}
}
