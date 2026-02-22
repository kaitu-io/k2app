//go:build windows

package config

import (
	"golang.org/x/sys/windows"
)

// isRoot reports whether the current process is running with administrator privileges.
// On Windows, this checks whether the process token belongs to the Administrators group.
func isRoot() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()

	elevated := token.IsElevated()
	return elevated
}
