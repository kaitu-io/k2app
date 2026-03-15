//go:build ios || android

package config

import "os"

func redirectStderrFD(_ *os.File) error {
	return nil // Mobile: stderr visible in Xcode console / logcat
}
