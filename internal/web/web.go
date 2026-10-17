// Package web holds the browser UI, embedded into the binary (ADR-0009,
// ADR-0024). The UI is plain HTML, CSS and ES modules; nothing is fetched from
// a CDN, so the binary serves it offline.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// FS returns the UI files, rooted so that "index.html" is at the top.
func FS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err) // the directory is embedded at compile time
	}
	return sub
}
