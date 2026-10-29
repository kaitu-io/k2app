//go:build linux && nowebapp

package webui

import "io/fs"

// webappFS is nil in nowebapp builds; NewWebappHandler returns nil.
var webappFS fs.FS = nil
