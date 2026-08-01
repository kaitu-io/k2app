package wire

import "testing"

func vOK() Validator {
	return func(token, udid, mode string) AuthVerdict {
		return AuthVerdict{OK: true, UserID: 7, SvcExp: 100}
	}
}
func vNoOpinion() Validator {
	return func(token, udid, mode string) AuthVerdict { return AuthVerdict{} }
}
func vReject(code int) Validator {
	return func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: code} }
}
func vUnreachable() Validator {
	return func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: -1} }
}

// TestChainValidators_FourState pins the four-state chain semantics (shared
// contract): OK returns immediately; explicit rejection returns immediately;
// -1 is remembered and the chain continues; no-opinion is skipped. Tail: -1 if
// seen, else explicit 401.
//
// The load-bearing rows are the "no-opinion must NOT mask" pair: shared nodes
// run with an EMPTY users file, so a users-file miss treated as explicit 401
// would shadow Center's real answer (including "unreachable") forever.
func TestChainValidators_FourState(t *testing.T) {
	cases := []struct {
		name string
		vs   []Validator
		want AuthVerdict
	}{
		{"first OK wins", []Validator{vOK(), vReject(401)}, AuthVerdict{OK: true, UserID: 7, SvcExp: 100}},
		{"no-opinion skips to OK", []Validator{vNoOpinion(), vOK()}, AuthVerdict{OK: true, UserID: 7, SvcExp: 100}},
		{"no-opinion must NOT mask remote rejection", []Validator{vNoOpinion(), vReject(402)}, AuthVerdict{Code: 402}},
		{"no-opinion must NOT mask remote unreachable", []Validator{vNoOpinion(), vUnreachable()}, AuthVerdict{Code: -1}},
		{"explicit rejection returns immediately", []Validator{vReject(403), vOK()}, AuthVerdict{Code: 403}},
		{"unreachable keeps going, later OK wins", []Validator{vUnreachable(), vOK()}, AuthVerdict{OK: true, UserID: 7, SvcExp: 100}},
		{"unreachable then no-opinion tail is -1", []Validator{vUnreachable(), vNoOpinion()}, AuthVerdict{Code: -1}},
		{"all no-opinion tail is explicit 401", []Validator{vNoOpinion(), vNoOpinion()}, AuthVerdict{Code: 401}},
		{"empty chain is explicit 401", nil, AuthVerdict{Code: 401}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ChainValidators(tc.vs...)("tok", "dev", "")
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
