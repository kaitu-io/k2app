package engine

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/kaitu-io/k2/wire"
)

// TestEchoStep_NotProbedEquivalentToUnsupported_Grid widens
// TestEchoStep_NotProbedMatchesUnsupportedArithmetic (one point, (2,2)) to the
// whole reachable state space: for every trust level (including the clamp at
// maxTrust and one past it) and every strike count up to and past the re-race
// threshold, a not-probed outcome must produce exactly the same NewTrust,
// NewStrikes and ShouldRerace as an unsupported one. Only Classify may differ.
// That is the "no behavior change in the echo loop" half of the
// ErrEchoNoConnection fix — a divergence anywhere on the grid would change
// probe cadence or re-race timing.
func TestEchoStep_NotProbedEquivalentToUnsupported_Grid(t *testing.T) {
	notProbed := []error{
		fmt.Errorf("%w: no cached connection", wire.ErrEchoNoConnection),
		// The relabelled shape QUICClient.RunEchoProbe returns for a dead conn.
		fmt.Errorf("%w: %w", wire.ErrEchoNoConnection, errors.New("echo: open stream: Application error 0x0 (local): connection reset")),
	}
	unsupported := []error{
		fmt.Errorf("echo: read response: %w", io.EOF),
		fmt.Errorf("echo: read response: %w", io.ErrUnexpectedEOF),
		errors.New("echo: read response: stream reset"),
	}
	maxTrust := int32(len(echoIntervals)) - 1

	for trust := int32(0); trust <= maxTrust+1; trust++ {
		for strikes := int32(0); strikes <= echoReraceThreshold+1; strikes++ {
			for _, u := range unsupported {
				ref := echoStep(trust, strikes, u)
				if ref.Classify != echoUnsupported {
					t.Fatalf("reference %q classified %v, want unsupported", u, ref.Classify)
				}
				for _, np := range notProbed {
					got := echoStep(trust, strikes, np)
					if got.Classify != echoNotProbed {
						t.Fatalf("(%d,%d) %q classified %v, want notProbed", trust, strikes, np, got.Classify)
					}
					if got.NewTrust != ref.NewTrust || got.NewStrikes != ref.NewStrikes || got.ShouldRerace != ref.ShouldRerace {
						t.Fatalf("(trust=%d, strikes=%d): notProbed %+v != unsupported %+v", trust, strikes, got, ref)
					}
				}
			}
		}
	}
}
