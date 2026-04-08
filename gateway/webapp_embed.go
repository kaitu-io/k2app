//go:build linux && !nowebapp

package gateway

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var webappEmbedFS embed.FS

var webappFS fs.FS = webappEmbedFS
