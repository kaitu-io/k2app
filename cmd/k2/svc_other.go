//go:build !windows

package main

// startDaemon runs as a foreground daemon on non-Windows platforms.
func startDaemon(configPath string) {
	runDaemon(configPath)
}
