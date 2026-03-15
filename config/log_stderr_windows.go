//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func redirectStderrFD(f *os.File) error {
	return windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}
