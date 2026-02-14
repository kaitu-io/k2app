package cloud

import (
	"embed"
	"io/fs"
)

//go:generate go run gen.go

//go:embed dist/*
var webappFS embed.FS

// FS returns the webapp filesystem rooted at dist/.
func FS() (fs.FS, error) {
	return fs.Sub(webappFS, "dist")
}
