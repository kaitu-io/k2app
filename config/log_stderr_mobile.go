//go:build ios || android

package config

import "os"

func redirectStderrFD(_ *os.File) (restore func(), err error) {
	return nil, nil // Mobile: stderr visible in Xcode console / logcat
}
