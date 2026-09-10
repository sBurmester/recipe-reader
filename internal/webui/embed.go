// Package webui embeds the built frontend into the binary so the whole
// application ships as a single file with no assets to deploy alongside it.
//
// The committed dist/index.html is a placeholder; `make frontend` replaces the
// directory with the real Vite build before a release build.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the built frontend, falling back to index.html for any path
// that is not a real file. The app itself routes on the URL hash, which never
// reaches the server, so the fallback is not what makes deep links work — it
// is what stops a typo or a stale bookmark from returning a bare 404 instead
// of the app.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TrimPrefix rather than slicing: URL.Path is normally rooted, but an
		// empty path would make an unchecked r.URL.Path[1:] panic, and fs.Stat
		// wants a relative name either way.
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" {
			if _, statErr := fs.Stat(sub, name); statErr != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
