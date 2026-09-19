package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/webui"
)

// Server timeouts. Without them a client that trickles a request in a byte at a
// time holds its connection — and a goroutine — for as long as it cares to, and
// enough of them exhaust the process. None of these bounds constrains a
// legitimate request: every handler answers from a handful of database round
// trips, and POST /api/import/run returns 202 before the import begins.
const (
	// readHeaderTimeout is the slowloris bound: the request line and headers
	// must arrive within it.
	readHeaderTimeout = 10 * time.Second
	// readTimeout bounds the whole request, headers and body together. Recipe
	// bodies are capped at 1 MiB in the api package, which arrives in well under
	// this on any link that can use the UI at all.
	readTimeout = 30 * time.Second
	// writeTimeout starts counting when the headers have been read, not when
	// the handler starts writing, so it covers the body read as well. Set below
	// readTimeout, a slow but legal upload would be cut off mid-response.
	writeTimeout = 60 * time.Second
	// idleTimeout closes keep-alive connections nobody is using. Left at zero
	// it silently inherits readTimeout; it is set on its own so the keep-alive
	// window is a decision rather than a side effect of the request bound.
	idleTimeout = 120 * time.Second
)

// newHTTPServer assembles the HTTP server: the API router under /api/ and the
// embedded frontend beneath it.
//
// It is separated from Run so the wiring can be exercised without a database,
// a signal handler or a listening socket — Run needs a database and a socket, and
// the composition root is where a mis-wired route or a missing middleware would
// otherwise go unnoticed until someone opened the page.
func newHTTPServer(cfg config.Config, deps api.Deps) (*http.Server, error) {
	// An unauthenticated deployment is reachable only from loopback —
	// config.Load refuses any other bind without a token — but it is still
	// worth naming at boot, because "it works without one" is how it stays
	// that way when the address later changes.
	if cfg.APIToken == "" {
		slog.Warn("no API_TOKEN configured; writes are unauthenticated and the server is bound to loopback only", "addr", cfg.HTTPAddr)
	}
	apiRouter := api.NewRouter(deps, api.Security{Token: cfg.APIToken, AllowedOrigins: cfg.CORSOrigins})

	frontend, err := webui.Handler()
	if err != nil {
		return nil, err
	}
	if webui.IsPlaceholder() {
		slog.Warn("serving the placeholder frontend, not a real build — run `make frontend` (or `make build`, which now depends on it) and rebuild")
	}

	// The embedded frontend takes everything the API does not claim. Go 1.22
	// ServeMux prefers the more specific "/api/" pattern, and does not strip
	// it, so the API router still sees the full path it registered.
	mux := http.NewServeMux()
	mux.Handle("/api/", apiRouter)
	mux.Handle("/", frontend)

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}, nil
}
