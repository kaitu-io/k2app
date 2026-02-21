package main

import (
	"fmt"
	"os"
)

// cmdSetup handles "k2s setup": ensures k2s is installed as a system service,
// starts it if needed, waits for the connect URL, and prints it.
func cmdSetup(args []string) {
	fmt.Fprintln(os.Stderr, "setup: not yet implemented")
	os.Exit(1)
}
