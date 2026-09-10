package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_Health(t *testing.T) {
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Errorf("body = %q", got)
	}
}

func TestRouter_RecoversFromPanic(t *testing.T) {
	router := NewRouter(Deps{})
	// A handler that panics must produce a 500, not crash the process.
	// handleListRecipes dereferences the nil Deps.Recipes interface — exercised
	// here deliberately to prove the recovery middleware turns a handler panic
	// into a response instead of taking the server down.
	req := httptest.NewRequest(http.MethodGet, "/api/recipes", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (recovered panic)", rec.Code)
	}
}

func TestRouter_SetsCORSHeaders(t *testing.T) {
	router := NewRouter(Deps{})
	req := httptest.NewRequest(http.MethodOptions, "/api/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "" {
		t.Error("expected Access-Control-Allow-Origin header to be set")
	}
}
