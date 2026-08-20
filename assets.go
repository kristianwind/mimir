package mimir

import (
	"embed"
	"io/fs"
)

// webDist is the built frontend bundle. It is embedded so the rune ships as
// one binary with no static-file volume to mount.
//
// The directory is committed with a placeholder index.html so `go build`
// works on a clean checkout before anyone has run `npm run build`.
//
//go:embed all:web/dist
var webDist embed.FS

// Web returns the frontend bundle rooted at web/dist.
func Web() (fs.FS, error) { return fs.Sub(webDist, "web/dist") }
