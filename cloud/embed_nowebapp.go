//go:build nowebapp

package cloud

import (
	"errors"
	"io/fs"
)

// FS returns an error when built with -tags nowebapp.
func FS() (fs.FS, error) {
	return nil, errors.New("cloud: webapp not embedded (built with nowebapp tag)")
}
