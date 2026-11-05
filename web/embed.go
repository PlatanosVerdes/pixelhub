// Package web holds the UI: static files that only use the public /api.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js i18n.js faces.js favicon.svg vendor
var files embed.FS

// FS is the UI's files, rooted at index.html.
func FS() fs.FS { return files }
