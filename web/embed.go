// Package web embeds the built React frontend (web/dist) into the Go binary
// so PC Sentinel ships as a single executable.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend, or false if `npm run build` hasn't been
// run (dist then only contains a placeholder).
func Dist() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
