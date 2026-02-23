//go:build windows

package wintun

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
)

//go:generate go run gen.go

//go:embed wintun.dll
var dllBytes []byte

var once sync.Once
var dllPath string

// EnsureExtracted extracts wintun.dll next to the running executable
// and returns the directory path. Called once per process.
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

		// Skip if already exists and same size.
		if info, e := os.Stat(target); e == nil && info.Size() == int64(len(dllBytes)) {
			dllPath = dir
			return
		}

		if e := os.WriteFile(target, dllBytes, 0o644); e != nil {
			err = e
			return
		}
		dllPath = dir
	})
	return dllPath, err
}
