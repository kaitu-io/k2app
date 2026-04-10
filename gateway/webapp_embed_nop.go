//go:build linux && nowebapp

package gateway

import "io/fs"

var webappFS fs.FS // nil — no webapp embedded
