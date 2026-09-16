// Package webui embeds the built frontend into the binary so the whole
// application ships as a single file with no assets to deploy alongside it.
//
// Two directories are embedded. dist/ is where `make frontend` puts the real
// Vite build, and it is gitignored down to a single .gitkeep so that
// //go:embed always has something to match. placeholder/ holds the page served
// when dist/ is empty — a build that has not had a frontend built into it.
//
// Keeping them apart is what makes the check below trustworthy: the placeholder
// lives outside the directory the build overwrites, so a `make build` cannot
// leave a real index.html sitting in the placeholder's place and quietly
// disable the warning.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

//go:embed placeholder
var placeholderFS embed.FS

// frontend returns the embedded filesystem to serve, and whether it is the
// placeholder rather than a real build.
func frontend() (fs.FS, bool, error) {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, false, err
	}
	if _, err := fs.Stat(dist, "index.html"); err == nil {
		return dist, false, nil
	}
	placeholder, err := fs.Sub(placeholderFS, "placeholder")
	if err != nil {
		return nil, false, err
	}
	return placeholder, true, nil
}

// IsPlaceholder reports whether this binary carries the placeholder page
// instead of a real frontend build.
//
// `make build` depends on `frontend`, so the Makefile path cannot produce one
// — but `go build ./...`, `go install`, and an IDE build all bypass the
// Makefile, and the binary they produce looks complete and is not. Answering
// this at startup turns that into a line in the log rather than a puzzle at
// the browser.
func IsPlaceholder() bool {
	_, placeholder, err := frontend()
	return err != nil || placeholder
}

// Handler serves the built frontend, falling back to index.html for any path
// that is not a real file. The app itself routes on the URL hash, which never
// reaches the server, so the fallback is not what makes deep links work — it
// is what stops a typo or a stale bookmark from returning a bare 404 instead
// of the app.
func Handler() (http.Handler, error) {
	sub, _, err := frontend()
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
