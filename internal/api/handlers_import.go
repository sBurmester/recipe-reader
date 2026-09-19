package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/sBurmester/recipe-reader/internal/pipeline"
)

// handleImportRun kicks off an import in the background and answers 202
// immediately. Extraction and storage need not be real-time, and doing the
// work inline would block the request for as long as the LLM and Instagram
// take — the status endpoint is how a caller finds out how it went.
//
// The run belongs to the worker, not to this request. It used to be a bare
// `go RunOnce(context.WithoutCancel(r.Context()))`: detached from the request,
// which was right, and from everything else, which was not — nothing waited
// for it at shutdown. Worker.Trigger starts it under the worker's own context,
// which shutdown cancels and then waits on. The Worker ignores a second trigger
// while one run is still in flight, so a double-click cannot start two imports.
func (d Deps) handleImportRun(w http.ResponseWriter, _ *http.Request) {
	if d.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "import worker not configured")
		return
	}
	// Refusing here rather than letting the worker skip silently: a 202 the
	// worker then ignores tells the caller a run started when none did, and
	// the status endpoint would go on reporting the previous run's tally.
	if cooldown := d.Worker.Cooldown(); cooldown > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(cooldown.Seconds()))))
		writeError(w, http.StatusTooManyRequests,
			"Instagram rate limit: imports resume in "+cooldown.Round(time.Second).String())
		return
	}
	d.Worker.Trigger()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

// handleImportStatus reports the in-flight flag plus the last run's tally. The
// error, if the last run had one, is surfaced as fields on a 200 rather than as
// an HTTP error status: the status call itself succeeded, and the frontend
// needs the tally alongside the failure.
//
// What it reports about that failure is a classification, never the error text.
// The chain behind Status.LastErr is wrapped around whatever the Instagram
// client returned and can carry endpoint paths, upstream response fragments and
// — with a database error in it — parts of the DSN; this endpoint has no
// authenticated callers to restrict that to. The full chain is logged where it
// is raised (Worker.RunOnce for a run, newFetcher for a startup login), so it
// is not logged again here: this handler is polled every couple of seconds
// while the import page is open, and one failure would otherwise fill the log
// with copies of itself.
func (d Deps) handleImportStatus(w http.ResponseWriter, _ *http.Request) {
	if d.Worker == nil {
		writeError(w, http.StatusServiceUnavailable, "import worker not configured")
		return
	}
	status := d.Worker.Status()
	resp := map[string]any{
		"running":  status.Running,
		"last_run": status.LastRun.Format(time.RFC3339),
		"seen":     status.LastResult.Seen,
		"imported": status.LastResult.Imported,
		"skipped":  status.LastResult.Skipped,
		// Reported apart from skipped and failed: a saved-posts feed
		// legitimately contains things that are not recipes, and how many is
		// the difference between "the importer is broken" and "most of what
		// you saved is not a recipe".
		"no_recipe": status.LastResult.NoRecipe,
		// Degraded counts posts extracted by the rules after the LLM call
		// failed. The tally would otherwise report those as ordinary
		// successes while quality silently dropped.
		"degraded": status.LastResult.Degraded,
		"failed":   status.LastResult.Failed,
	}
	if code, message := pipeline.Classify(status.LastErr); code != pipeline.FailureNone {
		// Two fields, because they answer two questions: error is the stable
		// code the frontend branches on, error_message the fixed sentence it
		// falls back to. Neither is derived from the error.
		resp["error"] = string(code)
		resp["error_message"] = message
	}
	// Present only while a rate limit is being waited out, so the UI can say
	// "paused until" rather than "ready" when a trigger would be refused.
	if cooldown := d.Worker.Cooldown(); cooldown > 0 {
		resp["cooldown_until"] = status.CooldownUntil.Format(time.RFC3339)
		resp["cooldown_seconds"] = int(math.Ceil(cooldown.Seconds()))
	}
	writeJSON(w, http.StatusOK, resp)
}
