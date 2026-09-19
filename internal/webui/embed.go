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

// contentSecurityPolicy is the policy the frontend is served under.
//
// It can be this strict because the bundle is: Vite emits one same-origin
// script and one same-origin stylesheet, with no inline script, no inline
// style, no style attribute, no image and no third-party origin — and
// embed_test.go fails if an inline script or style is ever added to
// index.html, rather than leaving the browser to block it silently.
//
//   - default-src 'self' covers the script, the stylesheet and the fetch calls
//     to /api, all same-origin.
//   - img-src allows data: for the odd inline icon a stylesheet may carry, and
//     not https:. The stored image_url is never rendered (security S8); if the
//     UI ever shows it, this is the line to change, and it should be changed
//     knowingly — rendering it pings Instagram's CDN on every page view.
//   - frame-ancestors 'none' is the clickjacking defence. The app was
//     framable, and framing plus a UI redress was an alternative route to the
//     destructive actions security S1 describes.
//   - object-src, base-uri and form-action close the usual gaps default-src
//     leaves open.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// withSecurityHeaders sets the response headers security S7 found missing on
// every response the frontend handler sends, the index.html fallback included.
// X-Frame-Options repeats frame-ancestors for browsers that predate it.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// The app links out to each recipe's Instagram permalink; no-referrer
		// keeps the local address it is served from out of those requests.
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// Handler serves the built frontend, falling back to index.html for any path
// that is not a real file. The app itself routes on the URL hash, which never
// reaches the server, so the fallback is not what makes deep links work — it
// is what stops a typo or a stale bookmark from returning a bare 404 instead
// of the app. Every response carries the security headers above.
func Handler() (http.Handler, error) {
	sub, _, err := frontend()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})), nil
}
