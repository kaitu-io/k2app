//go:build !windows

package main

import "syscall"

func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGKILL)
}
