package pipeline

import (
	"context"
	"errors"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// FailureCode is a stable, caller-safe name for why an import failed.
//
// It exists because the import status endpoint used to answer with
// Status.LastErr.Error() verbatim, and that chain is not built for a caller: it
// wraps whatever the Instagram client returned, which can carry the requested
// endpoint, fragments of the upstream response body, and — once a database
// error is in the chain — parts of the DSN. Every other handler in the API is
// deliberately opaque ("search failed", "create failed"); this turns the one
// exception into a classification the frontend can branch on and a sentence a
// person can read, while the full chain stays in the server log where it is
// actually useful.
//
// The codes are part of the HTTP contract: add to them, do not rename them.
type FailureCode string

const (
	// FailureNone is the zero value: no error to report.
	FailureNone FailureCode = ""
	// FailureRateLimited means Instagram is throttling the account. The worker
	// is standing down; Status.CooldownUntil says until when.
	FailureRateLimited FailureCode = "rate_limited"
	// FailureAuth means Instagram would not accept the stored session or the
	// configured credentials.
	FailureAuth FailureCode = "instagram_auth"
	// FailureSchemaDrift means Instagram answered in a shape the importer does
	// not recognise — an operator-visible signal that the unofficial client
	// needs updating, not something waiting will fix.
	FailureSchemaDrift FailureCode = "instagram_schema_drift"
	// FailureFetch means collecting the saved posts failed for some other
	// reason: a transport error, a timeout, an unexpected status.
	FailureFetch FailureCode = "fetch_failed"
	// FailureCancelled means the run was stopped before it finished, which is
	// the normal outcome of a shutdown during an import.
	FailureCancelled FailureCode = "cancelled"
	// FailureImport is the fallback for anything not classified above.
	FailureImport FailureCode = "import_failed"
)

// ErrFetch marks a failure to collect saved posts, as opposed to a failure
// while importing one of them. Run wraps every fetch failure with it so the
// distinction survives into Classify without anyone matching on message text.
var ErrFetch = errors.New("pipeline: fetch posts")

// ErrLogin marks a failure to authenticate with Instagram. The composition root
// wraps a failed startup login with it before handing it to
// Worker.RecordFailure, which is the one failure that reaches Status without a
// run behind it.
var ErrLogin = errors.New("instagram login failed")

// failureMessages are the sentences the API reports beside each code. They are
// fixed text chosen per code, never derived from the error, which is the whole
// point: nothing from the error chain reaches the caller.
var failureMessages = map[FailureCode]string{
	FailureRateLimited: "Instagram is rate-limiting this account; imports resume after a cooldown.",
	FailureAuth:        "Instagram did not accept the configured account; the next import retries the login.",
	FailureSchemaDrift: "Instagram returned a response the importer did not recognise.",
	FailureFetch:       "Fetching saved posts from Instagram failed.",
	FailureCancelled:   "The import was stopped before it finished.",
	FailureImport:      "The last import run failed.",
}

// Classify reduces an import error to a code and a fixed human-readable
// sentence. It returns FailureNone and an empty message for a nil error.
//
// Order matters: a rate limit and a re-auth are both reached through a fetch,
// so the specific causes are tested before ErrFetch, which would otherwise
// swallow them.
func Classify(err error) (FailureCode, string) {
	if err == nil {
		return FailureNone, ""
	}
	code := FailureImport
	switch {
	case errors.Is(err, instagram.ErrRateLimited):
		code = FailureRateLimited
	case errors.Is(err, instagram.ErrReauthRequired), errors.Is(err, ErrLogin):
		code = FailureAuth
	case errors.Is(err, instagram.ErrSchemaDrift):
		code = FailureSchemaDrift
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = FailureCancelled
	case errors.Is(err, ErrFetch):
		code = FailureFetch
	}
	return code, failureMessages[code]
}
