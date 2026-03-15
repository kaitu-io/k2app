//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func redirectStderrFD(f *os.File) (restore func(), err error) {
	origHandle, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE)
	if err != nil {
		return nil, err
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())); err != nil {
		return nil, err
	}
	return func() {
		windows.SetStdHandle(windows.STD_ERROR_HANDLE, origHandle) //nolint:errcheck
	}, nil
}
