package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
)

// installLog accumulates `k2 service install` diagnostics and best-effort
// flushes them to <log dir>/k2-service-install.log — the same directory the
// daemon logs to, so the desktop shell's feedback upload (which globs
// k2*.log in the service log dirs) picks it up automatically.
//
// Invariants:
//   - Never affects install control flow: every filesystem operation is
//     error-swallowed. A missing/unwritable log is itself a signal.
//   - Overwrites on each run: the file always describes the LATEST attempt.
type installLog struct {
	b strings.Builder
}

// logf appends one timestamped line. Also mirrors to stdout so the line is
// visible to whoever captures the CLI output (e.g. the NSIS install hook).
func (l *installLog) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	fmt.Fprintf(&l.b, "%s %s\r\n", time.Now().Format(time.RFC3339), line)
	fmt.Println(line)
}

// flush writes the accumulated lines. Best-effort by design.
func (l *installLog) flush() {
	l.flushTo(installLogPath())
}

// flushTo writes to an explicit path (split out for tests).
func (l *installLog) flushTo(path string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(l.b.String()), 0o644)
}

// installLogPath resolves next to the daemon's own log file: elevated →
// system dir (%ProgramData%\kaitu on Windows), otherwise the user log dir.
// Both locations are scanned by the desktop feedback upload.
func installLogPath() string {
	return config.DefaultLogPath("k2-service-install")
}
