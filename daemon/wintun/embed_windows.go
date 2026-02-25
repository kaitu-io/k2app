//go:build windows

package wintun

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

//go:generate go run gen.go

//go:embed wintun_amd64.dll
var dllAmd64 []byte

//go:embed wintun_arm64.dll
var dllArm64 []byte

var once sync.Once
var dllPath string

// EnsureExtracted extracts wintun.dll next to the running executable
// and returns the directory path. Called once per process.
//
// Both amd64 and arm64 DLLs are embedded. At runtime, we detect the
// native CPU architecture and extract the matching DLL. This is needed
// because k2.exe is always built as amd64 but may run under emulation
// on ARM64 Windows — wintun.dll is a kernel driver and must match the
// real hardware.
func EnsureExtracted() (string, error) {
	var err error
	once.Do(func() {
		exe, e := os.Executable()
		if e != nil {
			err = e
			return
		}
		dir := filepath.Dir(exe)
		target := filepath.Join(dir, "wintun.dll")

		dll := dllAmd64
		if isNativeARM64() {
			dll = dllArm64
		}

		// Skip if already exists and same size.
		if info, e := os.Stat(target); e == nil && info.Size() == int64(len(dll)) {
			dllPath = dir
			return
		}

		if e := os.WriteFile(target, dll, 0o644); e != nil {
			err = e
			return
		}
		dllPath = dir
	})
	return dllPath, err
}

// isNativeARM64 detects if the real CPU is ARM64, even when running
// under x86_64 emulation. Uses IsWow64Process2 (Windows 10 1709+).
func isNativeARM64() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("IsWow64Process2")
	if proc.Find() != nil {
		return false // API not available, assume amd64
	}

	var processMachine, nativeMachine uint16
	h, _ := syscall.GetCurrentProcess()
	ret, _, _ := proc.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&processMachine)),
		uintptr(unsafe.Pointer(&nativeMachine)),
	)
	if ret == 0 {
		return false
	}

	const imageFileMachineARM64 = 0xAA64
	return nativeMachine == imageFileMachineARM64
}
