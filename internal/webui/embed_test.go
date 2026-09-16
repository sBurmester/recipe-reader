// internal/webui/embed_test.go
package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These assert behaviour rather than page content, so they hold whether
// dist/ holds the committed placeholder or a real `make frontend` build.
func serve(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_ServesIndexAtRoot(t *testing.T) {
	rec := serve(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected a non-empty index.html")
	}
}

// An unknown path must return the app shell rather than a bare 404, so a
// stale bookmark or a typo still lands on the application.
func TestHandler_FallsBackToIndexForUnknownPath(t *testing.T) {
	root := serve(t, "/")
	rec := serve(t, "/recipes/999")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback to index.html)", rec.Code)
	}
	if rec.Body.String() != root.Body.String() {
		t.Error("fallback response differs from index.html")
	}
}

// An empty URL.Path must not panic: slicing the leading slash off without
// checking would, and a panicking file server takes the process down.
func TestHandler_EmptyPathDoesNotPanic(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = ""
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// A binary built without a frontend must say so. This is the guard that makes
// `go build ./...` — which bypasses the Makefile's frontend prerequisite —
// produce something diagnosable rather than a binary that looks complete and
// serves a placeholder page.
func TestIsPlaceholder_MatchesWhatIsServed(t *testing.T) {
	sub, placeholder, err := frontend()
	if err != nil {
		t.Fatalf("frontend() error = %v", err)
	}
	if placeholder != IsPlaceholder() {
		t.Errorf("IsPlaceholder() = %v, but frontend() reports placeholder = %v", IsPlaceholder(), placeholder)
	}

	// Whichever it is, index.html must exist: that is what Handler serves and
	// what the fallback rewrites to.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Errorf("index.html missing from the served filesystem: %v", err)
	}
}

// The placeholder lives outside dist/ precisely so that a `make build` cannot
// overwrite it and silently turn the warning off.
func TestPlaceholder_IsNotInsideTheBuildOutputDirectory(t *testing.T) {
	if _, err := fs.Stat(placeholderFS, "placeholder/index.html"); err != nil {
		t.Errorf("placeholder/index.html is missing: %v", err)
	}
}
