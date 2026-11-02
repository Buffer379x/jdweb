// Package web embeds the built web app. `pnpm build` writes it to web/dist; the
// directory holds only a placeholder in a checkout that was not built, and the
// server then explains that the web interface is missing.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built web app with index.html at its root.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: " + err.Error()) // dist is embedded, so the sub tree exists
	}
	return sub
}
