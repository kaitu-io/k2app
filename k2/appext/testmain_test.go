package appext

import (
	"fmt"
	"os"
	"testing"

	"github.com/kaitu-io/k2/config"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	code := m.Run()

	// Clean up the active stderr→lumberjack redirect goroutine before
	// goleak checks. SetupLogging may have been called by tests that
	// exercise Engine.Start() with a LogDir.
	config.CloseLogging()

	if code != 0 {
		os.Exit(code)
	}
	if err := goleak.Find(
		// lumberjack log rotation goroutine — starts on first log write, no public Close().
		goleak.IgnoreTopFunction("gopkg.in/lumberjack%2ev2.(*Logger).millRun"),
	); err != nil {
		fmt.Fprintf(os.Stderr, "goleak: %v\n", err)
		os.Exit(1)
	}
}
