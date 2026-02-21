package daemon

import (
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/kaitu-io/k2/config"
)

// Reload re-reads the config file at d.ConfigPath and applies dynamic field
// changes. Static field changes (listen, mode, tun, proxy.listen, pid_file) are
// logged as warnings but not applied — they require a daemon restart.
//
// Reload does NOT hold opMu during config file read. opMu is acquired only
// during the apply step to stay consistent with the concurrency model.
func (d *Daemon) Reload() error {
	if d.ConfigPath == "" {
		return fmt.Errorf("no config file to reload")
	}
	// Stub: not implemented.
	return fmt.Errorf("not implemented")
}

// applyDynamicConfig compares newCfg against the current lastConfig and:
//   - Warns about static field changes (listen, mode, tun, proxy.listen, pid_file).
//   - Applies dynamic field changes (server, dns, rule, log.level).
//   - Returns true if the server URL changed (caller may trigger reconnect).
//
// Caller must hold d.mu when calling this function.
func (d *Daemon) applyDynamicConfig(newCfg *config.ClientConfig) (serverChanged bool) {
	// Stub: not implemented.
	return false
}

// loggedStaticFields are field names that are static (require restart to change).
var loggedStaticFields = []string{"listen", "mode", "tun.ipv4", "tun.ipv6", "proxy.listen", "pid_file"}

// redirectLog replaces the standard log output with w and returns an opaque
// restore token. Used in tests to capture log output.
func redirectLog(w *strings.Builder) interface{} {
	type savedLog struct {
		flags  int
		writer io.Writer
	}
	orig := &savedLog{
		flags:  log.Flags(),
		writer: log.Writer(),
	}
	log.SetOutput(w)
	log.SetFlags(0)
	return orig
}

// restoreLog restores the standard logger from the token returned by redirectLog.
func restoreLog(orig interface{}) {
	type savedLog struct {
		flags  int
		writer io.Writer
	}
	if s, ok := orig.(*savedLog); ok {
		log.SetOutput(s.writer)
		log.SetFlags(s.flags)
	}
}
