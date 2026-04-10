//go:build linux && !nowebapp

// Package webui provides an embedded single-page application, encrypted
// key-value storage, platform info, and OTA upgrade for Kaitu Linux daemons
// (cmd/k2r and cmd/k2). Upper layers (gateway and daemon packages) register
// webui-returned handlers onto their own HTTP mux.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var webappEmbedFS embed.FS

// webappFS is the embedded webapp file system; nil when built with
// the nowebapp tag.
var webappFS fs.FS = webappEmbedFS
