//go:build windows

package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// getHardwareID reads HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid.
// Flag set matches Rust machine-uid 0.5.4 on 64-bit processes: plain
// QUERY_VALUE, no explicit WOW64_64KEY. On Windows Server 2025 the
// explicit WOW64_64KEY flag was observed to return a stale/alternate
// value that differs from what Rust reads.
func getHardwareID() (string, error) {
	k, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return "", fmt.Errorf("open Cryptography key: %w", err)
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return "", fmt.Errorf("read MachineGuid: %w", err)
	}
	return strings.TrimSpace(guid), nil
}
