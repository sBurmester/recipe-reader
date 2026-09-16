package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func health(t *testing.T, deps Deps) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(deps, testSecurity).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return body
}

// The build stamp is reported over HTTP because that is the only channel
// whoever is looking at a misbehaving deployment reliably has. Without it,
// identifying what is running means hashing the binary.
func TestHealth_ReportsTheVersion(t *testing.T) {
	body := health(t, Deps{Version: "v1.2.3-4-gabc1234"})

	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if body["version"] != "v1.2.3-4-gabc1234" {
		t.Errorf("version = %q, want the stamped build", body["version"])
	}
}

// Unstamped builds — `go build`, and every test — omit the field rather than
// reporting an empty string, which would read as "this build has no version"
// instead of "nobody asked for one".
func TestHealth_OmitsAnUnstampedVersion(t *testing.T) {
	body := health(t, Deps{})

	if _, ok := body["version"]; ok {
		t.Errorf("version field present on an unstamped build: %+v", body)
	}
}
