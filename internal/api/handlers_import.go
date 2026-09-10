package api

import (
	"context"
	"net/http"
	"time"
)

// handleImportRun kicks off an import in the background and answers 202
// immediately. Extraction and storage need not be real-time, and doing the
// work inline would block the request for as long as the LLM and Instagram
// take — the status endpoint is how a caller finds out how it went.
//
// The run gets a context detached from the request: r.Context() is cancelled
// the moment this handler returns, which would abort the import before it did
// anything. WithoutCancel keeps any request-scoped values while dropping that
// cancellation. The Worker itself ignores a second trigger while one run is
// still in flight, so a double-click cannot start two imports.
func (d Deps) handleImportRun(w http.ResponseWriter, r *http.Request) {
	if d.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "import worker not configured")
		return
	}
	ctx := context.WithoutCancel(r.Context())
	go d.Worker.RunOnce(ctx)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// handleImportStatus reports the in-flight flag plus the last run's tally. The
// error, if the last run had one, is surfaced as a string field rather than an
// HTTP error status: the status call itself succeeded, and the frontend needs
// the tally alongside the failure.
func (d Deps) handleImportStatus(w http.ResponseWriter, _ *http.Request) {
	if d.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "import worker not configured")
		return
	}
	lastRun, lastResult, lastErr, running := d.Worker.Status()
	resp := map[string]any{
		"running":  running,
		"last_run": lastRun.Format(time.RFC3339),
		"seen":     lastResult.Seen,
		"imported": lastResult.Imported,
		"skipped":  lastResult.Skipped,
		"failed":   lastResult.Failed,
	}
	if lastErr != nil {
		resp["error"] = lastErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}
