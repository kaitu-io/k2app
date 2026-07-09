package center

import "testing"

func TestIsUserBlocked(t *testing.T) {
	cases := []struct {
		name string
		user *User
		want bool
	}{
		{"nil user", nil, false},
		{"nil flag", &User{}, false},
		{"false flag", &User{IsBlocked: BoolPtr(false)}, false},
		{"true flag", &User{IsBlocked: BoolPtr(true)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUserBlocked(tc.user); got != tc.want {
				t.Errorf("isUserBlocked(%+v) = %v, want %v", tc.user, got, tc.want)
			}
		})
	}
}
