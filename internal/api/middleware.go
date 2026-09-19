package api

import (
	"crypto/subtle"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

// Security carries the API's access controls.
//
// Token is the bearer token every state-changing request must present; an
// empty Token disables the check, which config.Load only permits on a loopback
// bind. AllowedOrigins is the set of browser origins allowed to read this
// API's responses — the bundled frontend is same-origin and needs none of
// them, so this exists for the Vite dev server and for any separately hosted
// UI.
type Security struct {
	Token          string
	AllowedOrigins []string
}

// withLogging records one line per request after the handler has run, so the
// logged duration covers the whole handler chain below it.
//
// The line carries the response status. Without it every request logged
// identically, 500s included, and the log could say that something was asked
// but never how it went.
//
// A health probe that succeeds is logged at debug level, below the default. The
// image's HEALTHCHECK asks every 30 seconds — nearly three thousand identical
// lines a day, burying the ones worth reading. A probe that fails is still
// logged at info, because that one is news.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.statusCode()
		level := slog.LevelInfo
		if r.URL.Path == healthPath && status == http.StatusOK {
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request", "method", r.Method, "path", r.URL.Path,
			"status", status, "duration", time.Since(start))
	})
}

// statusRecorder remembers the status a handler sent, for withLogging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the first final status. An informational 1xx is not the
// response's status — the real one follows it — so it is passed on unrecorded.
func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 && code >= http.StatusOK {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write records the implicit 200 net/http sends when a handler writes a body
// without calling WriteHeader first.
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer — Flush,
// the per-request deadlines — through the wrapper.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// statusCode is the status the client received. A handler that wrote nothing
// at all still answered 200: net/http sends it when the handler returns.
func (s *statusRecorder) statusCode() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// withRecovery turns a handler panic into a 500 response. net/http already
// keeps a panicking handler from killing the process, but it drops the
// connection without a response; answering with JSON instead means a client
// bug or an unexpected nil never looks like a network failure to the frontend.
//
// http.ErrAbortHandler is the exception, and is re-panicked. It is not a bug:
// it is how a handler — or httputil.ReverseProxy — deliberately aborts a
// response, and net/http recognises it and drops the connection without logging
// a stack. Answering it with a 500 would send a response the handler chose not
// to send, and log a deliberate abort as a crash.
//
// If the handler had already written a response body before panicking the
// status is fixed by then and only the log line records what happened.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// Compared by identity, as net/http itself does: a wrapped
				// abort is not one net/http would recognise either.
				if rec == http.ErrAbortHandler { //nolint:errorlint // see above
					panic(rec)
				}
				slog.Error("panic recovered", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withCORS answers preflight requests and echoes the request Origin back only
// when it appears in allowed.
//
// The previous `Access-Control-Allow-Origin: *` was reasoned about as safe
// because the API carries no cookies. That premise is true and the conclusion
// does not follow: this service's authorization model is "you can reach it",
// and a browser reaches it on the victim's behalf. With `*`, any page the user
// visited could read the whole collection, and its preflight for DELETE was
// answered 204 from any origin.
//
// A request with no Origin header — the bundled same-origin frontend, curl, a
// health probe — passes through untouched. CORS is a browser mechanism and has
// nothing to say about those; withAuth is what guards them.
func withCORS(allowed []string, next http.Handler) http.Handler {
	index := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		if origin = strings.TrimSpace(origin); origin != "" {
			index[origin] = struct{}{}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Vary is set whether or not the origin matched: a cache that served
		// one origin's response to another would undo the allowlist.
		w.Header().Add("Vary", "Origin")

		origin := r.Header.Get("Origin")
		if _, ok := index[origin]; ok && origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
		}

		// Preflight is answered here either way. Without the headers above the
		// browser reads the 204 as a refusal, which is the intended answer for
		// an origin that is not on the list.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withAuth requires a bearer token on every state-changing request when one is
// configured, and lets reads through unauthenticated.
//
// Splitting it that way is deliberate: the collection is one person's recipes
// rather than a secret, and the exposure worth closing is a page the user
// happens to visit issuing writes — enumerating ids and deleting the lot —
// using the user's own network position as the credential.
//
// An empty token disables the check entirely. That combination is confined to
// a loopback bind by Config.validate, so it cannot be the accidental state of
// a network-reachable deployment.
func withAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || !isMutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		// ConstantTimeCompare also returns 0 for a length mismatch, so neither
		// the token's contents nor its length leak through response timing.
		if subtle.ConstantTimeCompare([]byte(bearerToken(r)), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="recipe-reader"`)
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withJSONWrites rejects a state-changing request that does not declare a JSON
// body.
//
// This is not about parsing — it is what makes the allowlist above reach the
// write path at all. `application/json` is not one of the three CORS-"simple"
// content types, so requiring it forces a preflight that withCORS then answers
// against the allowlist. Without it, a page on any origin can POST here with
// `Content-Type: text/plain` and never be preflighted: handleCreateRecipe
// never inspected the header, and handleImportRun reads no body at all.
func withJSONWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isMutating reports whether the method changes server state. OPTIONS is not
// in the list and never reaches these checks anyway — withCORS answers
// preflight above them, which is what keeps preflight from needing a token.
func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// bearerToken extracts the credential from an Authorization header, or returns
// "" when the header is absent or uses another scheme. The scheme name is
// matched case-insensitively, as RFC 7235 requires.
func bearerToken(r *http.Request) string {
	const scheme = "bearer "
	header := r.Header.Get("Authorization")
	if len(header) < len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(header[len(scheme):])
}
