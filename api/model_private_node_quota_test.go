package center

import "testing"

func TestValidatePrivateNodeQuotaInvariant(t *testing.T) {
	cases := []struct {
		name           string
		trafficTotal   int64
		bundleTransfer int64
		wantErr        bool
	}{
		{"sold below bundle ok", 1 * 1e12, 2 * 1e12, false},
		{"sold equals bundle rejected", 2 * 1e12, 2 * 1e12, true},
		{"sold above bundle rejected", 3 * 1e12, 2 * 1e12, true},
		{"zero bundle rejected", 1 * 1e12, 0, true},
		{"negative bundle rejected", 1 * 1e12, -1, true},
		{"zero sold rejected", 0, 2 * 1e12, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePrivateNodeQuotaInvariant(tc.trafficTotal, tc.bundleTransfer)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate(%d,%d): err=%v wantErr=%v", tc.trafficTotal, tc.bundleTransfer, err, tc.wantErr)
			}
		})
	}
}
