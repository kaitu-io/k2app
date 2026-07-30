package wire

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestRaceExitError_PreservesStructuredCause: the race exit used to flatten
// candidate errors with %v, so a metadata rejection (the ONLY way an enforce
// rollout failure reaches engine.Start) degraded to an unclassifiable string.
func TestRaceExitError_PreservesStructuredCause(t *testing.T) {
	authErr := fmt.Errorf("%w: %w", ErrMetadataRejected, &StreamError{Code: 402, Message: "subscription expired"})
	err := raceExitError(authErr, nil, errors.New("tcpws: dial refused"))
	if err == nil {
		t.Fatal("want non-nil")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("ErrMetadataRejected lost through race exit: %v", err)
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Code != 402 {
		t.Fatalf("StreamError code lost through race exit: %v", err)
	}
}

func TestRaceExitError_AllNil(t *testing.T) {
	if err := raceExitError(nil, nil, nil); err == nil {
		t.Fatal("all-nil (timeout without candidate errors) must still be an error")
	}
}

// TestRaceTransport_MetadataRejectionSurvivesExit is the end-to-end twin of
// TestRaceExitError_PreservesStructuredCause: it drives a REAL RaceTransport
// against an enforce server whose validator always rejects 402, and proves the
// structured cause survives BOTH race exits. The pure unit test cannot catch a
// return site left on %v (nor the raceCtx.Done() branch); this can.
//
// wire cannot import engine, so the engine.ClassifyError(err) == {402, client}
// leg is pinned by TestClassifyError_MetadataRejection (Step 4) on the exact
// same error shape. Together they cover "race exit -> engine classification"
// end to end. (needs imports: context, errors, time — errors/fmt already there.)
func TestRaceTransport_MetadataRejectionSurvivesExit(t *testing.T) {
	_, addr, cleanup := startRawQUICServer(t, vReject(402), true) // enforce=true
	defer cleanup()

	cfg := Config{Host: "127.0.0.1", Port: addr.Port, Insecure: true, UDID: "d", Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := RaceTransport(ctx, cfg, "", nil)
	if res != nil && res.Dialer != nil {
		res.Dialer.Close()
	}
	if err == nil {
		t.Fatal("enforce reject 402 must fail the race")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("ErrMetadataRejected lost through RaceTransport exit: %v", err)
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Code != 402 {
		t.Fatalf("StreamError{402} lost through RaceTransport exit: %v", err)
	}
}
