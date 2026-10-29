//go:build windows

package main

// daemonNotRunningHint returns a platform-aware hint for the daemon-not-running error.
func daemonNotRunningHint() string {
	return "run 'k2 setup <URL>' or 'sc start kaitu'"
}
