package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// securedRouter is the deployment S1 asks for: a token in front of the writes
// and a single allowed browser origin.
func securedRouter() http.Handler {
	return NewRouter(Deps{}, Security{
		Token:          "s3cret-token",
		AllowedOrigins: []string{"https://recipes.example"},
	})
}

func TestWithAuth_RejectsUnauthenticatedWrite(t *testing.T) {
	// DELETE is the one that motivated the finding: with no auth and a
	// wildcard origin, a page could enumerate ids and empty the collection.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := jsonRequest(method, "/api/recipes/1", nil)
		rec := httptest.NewRecorder()
		securedRouter().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got == "" {
			t.Errorf("%s: expected a WWW-Authenticate challenge", method)
		}
	}
}

func TestWithAuth_RejectsWrongToken(t *testing.T) {
	req := jsonRequest(http.MethodDelete, "/api/recipes/1", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	rec := httptest.NewRecorder()

	securedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// The scheme name is case-insensitive per RFC 7235, and a client that sends
// "bearer" must not be rejected for it.
func TestWithAuth_AcceptsCorrectTokenAnySchemeCase(t *testing.T) {
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		req := jsonRequest(http.MethodPost, "/api/import/run", nil)
		req.Header.Set("Authorization", scheme+" s3cret-token")
		rec := httptest.NewRecorder()

		securedRouter().ServeHTTP(rec, req)

		// No worker is configured on this router, so passing the token gets as
		// far as the handler's own 503 — which is the point: it got past auth.
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503 (past auth, no worker configured)", scheme, rec.Code)
		}
	}
}

// Reads stay open: the collection is one person's recipes, and the exposure
// worth closing is a foreign page issuing writes on their behalf.
func TestWithAuth_LeavesReadsOpen(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()

	securedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// Preflight must never require the token — the browser sends it without one,
// and a 401 here would break every legitimate cross-origin write.
func TestWithAuth_PreflightNeedsNoToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/api/recipes", nil)
	req.Header.Set("Origin", "https://recipes.example")
	rec := httptest.NewRecorder()

	securedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://recipes.example" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

// This is chair correction 7: handleCreateRecipe never inspected Content-Type
// and handleImportRun reads no body, so a text/plain POST was a CORS-simple
// request that skipped preflight entirely. Rejecting it is what puts the write
// path behind the origin allowlist.
func TestWithJSONWrites_RejectsCORSSimpleContentType(t *testing.T) {
	for _, contentType := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/import/run", nil)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()

		NewRouter(Deps{}, testSecurity).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status = %d, want 415", contentType, rec.Code)
		}
	}
}

func TestWithJSONWrites_AcceptsJSONWithCharset(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/import/run", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	NewRouter(Deps{}, testSecurity).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (past the media-type guard, no worker configured)", rec.Code)
	}
}

func TestWithCORS_DoesNotEchoUnknownOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	securedRouter().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for an origin not on the allowlist", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin even on a refusal", got)
	}
}

// The same-origin bundled frontend, curl and health probes send no Origin.
// CORS has nothing to say about those, and they must not be answered with a
// header naming the empty origin.
func TestWithCORS_PassesThroughRequestWithoutOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()

	securedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty when no Origin was sent", got)
	}
}
