//go:build windows

package main

func isProcessAlive(pid int) bool { return false }

func killProcess(pid int) {}
