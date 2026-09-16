package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouter_Health(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)
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
	router := NewRouter(Deps{}, testSecurity)
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

func TestRouter_SetsCORSHeadersForAllowedOrigin(t *testing.T) {
	router := NewRouter(Deps{}, testSecurity)
	req := httptest.NewRequest(http.MethodOptions, "/api/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the request origin echoed back", got)
	}
	// Without Vary, a shared cache could hand one origin's response — headers
	// included — to a different one, which would undo the allowlist.
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
}
