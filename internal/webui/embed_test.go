// internal/webui/embed_test.go
package webui

import (
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
