//go:build !windows

package main

import (
	"fmt"
	"io"
)

// runSignal sends a signal (stop or reload) to the server identified by pidPath.
// It writes a confirmation message to w on success.
// Returns an error if the PID file is missing, the process is not alive, or signal fails.
func runSignal(sig string, pidPath string, w io.Writer) error {
	return fmt.Errorf("not yet implemented")
}
