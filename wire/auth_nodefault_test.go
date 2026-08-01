package wire

import (
	"testing"
	"time"
)

// Node-fault verdict (AuthCodeNodeFault) end-to-end through the chain and the
// metadata decision. The property under test throughout: a node that cannot
// authenticate must never be reported as the DEVICE's credentials failing.

func constVerdict(v AuthVerdict) Validator {
	return func(string, string, string) AuthVerdict { return v }
}

// TestChain_NodeFaultOutranksUndetermined pins the tail precedence. A node
// fault is permanent (an operator must fix it); -1 is transient. Collapsing
// the two would disguise a broken node as a passing Center blip forever.
func TestChain_NodeFaultOutranksUndetermined(t *testing.T) {
	tests := []struct {
		name string
		vs   []Validator
		want int
	}{
		{
			name: "node fault beats undetermined regardless of order",
			vs: []Validator{
				constVerdict(AuthVerdict{Code: AuthCodeUndetermined}),
				constVerdict(AuthVerdict{Code: AuthCodeNodeFault}),
			},
			want: AuthCodeNodeFault,
		},
		{
			name: "node fault beats undetermined, reversed",
			vs: []Validator{
				constVerdict(AuthVerdict{Code: AuthCodeNodeFault}),
				constVerdict(AuthVerdict{Code: AuthCodeUndetermined}),
			},
			want: AuthCodeNodeFault,
		},
		{
			name: "node fault does NOT mask an explicit Center rejection",
			vs: []Validator{
				constVerdict(AuthVerdict{Code: AuthCodeNodeFault}),
				constVerdict(AuthVerdict{Code: 402}),
			},
			want: 402,
		},
		{
			name: "no-opinion only still fail-closes to 401",
			vs: []Validator{
				constVerdict(AuthVerdict{}),
				constVerdict(AuthVerdict{}),
			},
			want: 401,
		},
		{
			name: "undetermined alone still returns -1",
			vs:   []Validator{constVerdict(AuthVerdict{Code: AuthCodeUndetermined})},
			want: AuthCodeUndetermined,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ChainValidators(tc.vs...)("t", "u", "")
			if got.OK {
				t.Fatalf("expected a non-OK verdict, got %+v", got)
			}
			if got.Code != tc.want {
				t.Errorf("chain tail Code = %d, want %d", got.Code, tc.want)
			}
		})
	}
}

// TestChain_NodeFaultYieldsToSuccess: a chain where an earlier validator is
// broken but a later one authenticates must still admit. The node-fault flag
// is a tail rule, never a short circuit.
func TestChain_NodeFaultYieldsToSuccess(t *testing.T) {
	v := ChainValidators(
		constVerdict(AuthVerdict{Code: AuthCodeNodeFault}),
		constVerdict(AuthVerdict{OK: true, UserID: 7}),
	)
	got := v("t", "u", "")
	if !got.OK || got.UserID != 7 {
		t.Fatalf("a later OK must win over an earlier node fault, got %+v", got)
	}
}

// TestResolveMetadataAuth_NodeFault pins the two properties that keep the
// enforce rollout honest when a node is misconfigured:
//
//  1. NOT authed in either mode — a node whose auth is broken must never
//     attribute traffic or land in an authed counter bucket.
//  2. The client-visible code is 5xx (server), never 4xx (client).
func TestResolveMetadataAuth_NodeFault(t *testing.T) {
	v := constVerdict(AuthVerdict{Code: AuthCodeNodeFault})
	for _, enforce := range []bool{false, true} {
		got := resolveMetadataAuth(nil, v, MetadataFrame{UDID: "dev1", Token: "t"}, enforce, time.Now())
		if got.authed {
			t.Errorf("enforce=%v: node fault must never be authed, got %+v", enforce, got)
		}
		if got.code != metadataRejectCodeNodeFault {
			t.Errorf("enforce=%v: code = %d, want %d (5xx server range)",
				enforce, got.code, metadataRejectCodeNodeFault)
		}
		if got.code >= 400 && got.code < 500 {
			t.Errorf("enforce=%v: node fault leaked into the 4xx CLIENT range as %d", enforce, got.code)
		}
		if got.udid != "" {
			t.Errorf("enforce=%v: node fault must not attribute a udid, got %q", enforce, got.udid)
		}
	}
}

// TestResolveMetadataAuth_NodeFaultCountsUnauthed guards the rollout metric
// that gates the enforce decision: a misconfigured node must show up as
// `unauthed`, not as a successful `cold` auth. Getting this backwards would
// make a node that authenticates nobody report a perfect success rate.
func TestResolveMetadataAuth_NodeFaultCountsUnauthed(t *testing.T) {
	st := resolveMetadataAuth(nil, constVerdict(AuthVerdict{Code: AuthCodeNodeFault}),
		MetadataFrame{UDID: "dev1", Token: "t"}, false, time.Now())

	r := NewRolloutStats()
	recordVerdict(r, st.cookieHit, st.authed)
	snap := r.snapshotAndReset()

	if snap.unauthed != 1 {
		t.Errorf("unauthed = %d, want 1", snap.unauthed)
	}
	if snap.cold != 0 || snap.cookieHit != 0 {
		t.Errorf("node fault must not reach an authed bucket: cold=%d cookieHit=%d", snap.cold, snap.cookieHit)
	}
}
