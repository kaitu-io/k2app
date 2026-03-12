package appext

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// lumberjack log rotation goroutine — starts on first log write, no public Close().
		goleak.IgnoreTopFunction("gopkg.in/lumberjack%2ev2.(*Logger).millRun"),
	)
}
