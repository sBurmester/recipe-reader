package api

import (
	"log/slog"
	"net/http"
	"time"
)

// withLogging records one line per request after the handler has run, so the
// logged duration covers the whole handler chain below it.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

// withRecovery turns a handler panic into a 500 response. net/http already
// keeps a panicking handler from killing the process, but it drops the
// connection without a response; answering with JSON instead means a client
// bug or an unexpected nil never looks like a network failure to the frontend.
//
// If the handler had already written a response body before panicking the
// status is fixed by then and only the log line records what happened.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withCORS allows any origin: the API serves its own bundled frontend in
// production and a Vite dev server on another port in development, and it
// exposes no cookies or credentials that a permissive origin could leak.
// Preflight requests are answered here and never reach the mux.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
