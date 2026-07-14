// Package web embeds the built React UI so the whole app ships as one binary.
// During development the PatternFly app lives under web/app and is built into
// web/dist by Vite; go:embed picks up whatever is in dist at build time.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded static assets.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
