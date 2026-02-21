package main

// This file holds stub implementations used during RED phase.
// Will be replaced by real implementations in GREEN phase.
// NOTE: These stubs intentionally return wrong values to produce FAIL (not compile error).

import (
	"fmt"
	"io"
)

// runTestConfig loads and validates the config at cfgPath, writing results to w.
// Returns nil if config is valid, error otherwise.
func runTestConfig(cfgPath string, w io.Writer) error {
	// RED stub — always returns error (tests expect success for valid config)
	return fmt.Errorf("not implemented")
}

// runSignal sends a signal (stop or reload) to the daemon identified by pidPath.
// Returns an error if the PID file is missing, stale, or signal fails.
func runSignal(sig string, pidPath string, w io.Writer) error {
	// RED stub — returns nil (tests expect error for missing/stale PID)
	return nil
}

// deprecatedWarn prints a deprecation warning for oldCmd to w, indicating the new replacement.
func deprecatedWarn(oldCmd string, newCmd string, w io.Writer) {
	// RED stub — prints nothing (tests expect a warning message)
}
