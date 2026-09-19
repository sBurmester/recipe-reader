package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/domain"
	"github.com/sBurmester/recipe-reader/internal/repository"
)

// captureLog points the default logger at a buffer for the rest of the test.
// No test in this package calls t.Parallel, which is what makes swapping the
// process-wide logger safe.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// requestLine returns the "http request" line withLogging wrote, or fails.
func requestLine(t *testing.T, log *bytes.Buffer) string {
	t.Helper()
	for line := range strings.Lines(log.String()) {
		if strings.Contains(line, `msg="http request"`) {
			return line
		}
	}
	t.Fatalf("no request line was logged; log:\n%s", log.String())
	return ""
}

// go #12: every request used to log identically, 500s included.
func TestWithLogging_RecordsTheResponseStatus(t *testing.T) {
	log := captureLog(t)

	rec := httptest.NewRecorder()
	NewRouter(Deps{}, testSecurity).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recipes/not-a-number", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if line := requestLine(t, log); !strings.Contains(line, "status=400") {
		t.Errorf("request line = %q, want status=400", line)
	}
}

// A handler that never calls WriteHeader still answered: net/http sends 200
// for it. Logging 0 there would be a status no client ever saw.
func TestWithLogging_ImplicitStatusIs200(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"body without WriteHeader": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) },
		"nothing written at all":   func(http.ResponseWriter, *http.Request) {},
	} {
		t.Run(name, func(t *testing.T) {
			log := captureLog(t)
			withLogging(handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
			if line := requestLine(t, log); !strings.Contains(line, "status=200") {
				t.Errorf("request line = %q, want status=200", line)
			}
		})
	}
}

// A 1xx is sent ahead of the response, not as it; the final status is the one
// that belongs in the log.
func TestWithLogging_SkipsInformationalStatus(t *testing.T) {
	log := captureLog(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
	})

	withLogging(handler).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil))

	if line := requestLine(t, log); !strings.Contains(line, "status=201") {
		t.Errorf("request line = %q, want status=201", line)
	}
}

// The recorder must not hide the writer's optional interfaces: a handler that
// flushes through http.ResponseController has to reach the real writer.
func TestStatusRecorder_UnwrapsForResponseController(t *testing.T) {
	inner := httptest.NewRecorder()
	if err := http.NewResponseController(&statusRecorder{ResponseWriter: inner}).Flush(); err != nil {
		t.Fatalf("Flush through the recorder: %v", err)
	}
	if !inner.Flushed {
		t.Error("the underlying writer was not flushed")
	}
}

// Logging used to sit inside recovery, so a panic unwound straight past it and
// a crashed request left no request line at all. It now sits outside, and
// records the 500 recovery answered with.
func TestRouter_PanickedRequestIsLoggedAs500(t *testing.T) {
	log := captureLog(t)

	// Deps.Recipes is nil, so the handler panics on its first call.
	rec := httptest.NewRecorder()
	NewRouter(Deps{}, testSecurity).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/recipes", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if line := requestLine(t, log); !strings.Contains(line, "status=500") {
		t.Errorf("request line = %q, want status=500", line)
	}
}

// go #11: http.ErrAbortHandler is a deliberate abort, not a crash. Recovery
// must let it through to net/http, which drops the connection quietly, rather
// than answer a response the handler chose not to send.
func TestWithRecovery_RepanicsErrAbortHandler(t *testing.T) {
	log := captureLog(t)
	aborting := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	rec := httptest.NewRecorder()

	recovered := func() (v any) {
		defer func() { v = recover() }()
		withRecovery(aborting).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return nil
	}()

	if recovered != http.ErrAbortHandler { //nolint:errorlint // identity is what net/http checks
		t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", recovered)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a response was written for an aborted handler: %q", rec.Body.String())
	}
	if strings.Contains(log.String(), "panic recovered") {
		t.Error("a deliberate abort was logged as a recovered panic")
	}
}

// failingLookups fails every call, the way a lost database connection does.
type failingLookups struct{ repository.LookupRepository }

var errLookupBackend = errors.New("conn closed: pq detail the client must not see")

func (failingLookups) ListCategories(context.Context) ([]domain.Category, error) {
	return nil, errLookupBackend
}

// A 500 used to discard its error outright. The client still sees only the
// generic message; the log now carries the cause.
func TestInternalError_LogsTheCauseButDoesNotSendIt(t *testing.T) {
	log := captureLog(t)

	rec := httptest.NewRecorder()
	NewRouter(Deps{Lookups: failingLookups{}}, testSecurity).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/categories", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "pq detail") {
		t.Errorf("the response leaks the internal error: %s", rec.Body.String())
	}
	if !strings.Contains(log.String(), "pq detail") {
		t.Errorf("the cause of the 500 was not logged; log:\n%s", log.String())
	}
}

// The image's HEALTHCHECK probes every 30 seconds. A probe that succeeds would
// otherwise be nearly three thousand identical info lines a day; it is logged
// at debug instead, while a probe that fails stays visible.
func TestWithLogging_KeepsSuccessfulHealthProbesOutOfTheInfoLog(t *testing.T) {
	log := captureLog(t) // the text handler's default level is info

	NewRouter(Deps{}, testSecurity).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	if strings.Contains(log.String(), "http request") {
		t.Errorf("a successful health probe was logged at info:\n%s", log.String())
	}

	// A request to the same path that is refused — a POST, turned away by the
	// JSON-write guard — is not a successful probe, and is logged like any other.
	NewRouter(Deps{}, testSecurity).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/healthz", nil))
	if !strings.Contains(log.String(), "path=/api/healthz") {
		t.Errorf("an unsuccessful request to the health path was not logged:\n%s", log.String())
	}
}
