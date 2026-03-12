//go:build windows

package main

// daemonNotRunningHint returns a platform-aware error message shown when
// k2 ctl cannot connect to the daemon (connection refused).
func daemonNotRunningHint() string {
	return `Error: daemon not running

  First time?     k2 setup <URL>
  Already set up? sc start kaitu

`
}
