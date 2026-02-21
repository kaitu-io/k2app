//go:build windows

package main

import (
	"fmt"
	"io"
)

// runSignal sends a signal (stop) to the server identified by pidPath on Windows.
// Note: Windows does not support SIGHUP (reload) — only stop is supported.
func runSignal(sig string, pidPath string, w io.Writer) error {
	return fmt.Errorf("not yet implemented")
}
