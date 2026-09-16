package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

func TestClassify(t *testing.T) {
	// The secret-ish string every case wraps: whatever Classify returns, none
	// of this may be in it.
	const leak = "https://i.instagram.com/api/v1/feed/saved/?max_id=QVFE token=hunter2"

	tests := []struct {
		name string
		err  error
		want FailureCode
	}{
		{"nil", nil, FailureNone},
		{
			"rate limit through a fetch",
			fmt.Errorf("%w: %w", ErrFetch, fmt.Errorf("%w: %s", instagram.ErrRateLimited, leak)),
			FailureRateLimited,
		},
		{
			"reauth through a fetch",
			fmt.Errorf("%w: %w", ErrFetch, fmt.Errorf("%w: %s", instagram.ErrReauthRequired, leak)),
			FailureAuth,
		},
		{
			"startup login",
			fmt.Errorf("%w at startup: %s", ErrLogin, leak),
			FailureAuth,
		},
		{
			"schema drift",
			fmt.Errorf("%w: %w", ErrFetch, fmt.Errorf("%w: %s", instagram.ErrSchemaDrift, leak)),
			FailureSchemaDrift,
		},
		{
			"shutdown mid-run",
			fmt.Errorf("pipeline: %w", context.Canceled),
			FailureCancelled,
		},
		{
			"plain fetch failure",
			fmt.Errorf("%w: %s", ErrFetch, leak),
			FailureFetch,
		},
		{
			"anything else",
			errors.New(leak),
			FailureImport,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, message := Classify(tc.err)
			if code != tc.want {
				t.Errorf("code = %q, want %q", code, tc.want)
			}
			if tc.err == nil {
				if message != "" {
					t.Errorf("message = %q, want empty for a nil error", message)
				}
				return
			}
			if message == "" {
				t.Errorf("code %q has no message", code)
			}
			if strings.Contains(message, "hunter2") || strings.Contains(message, "instagram.com") {
				t.Errorf("message = %q, which carries the error chain it was supposed to replace", message)
			}
		})
	}
}

// A rate limit and a re-auth both arrive wrapped in ErrFetch, so the order of
// the cases decides whether the specific cause survives. Asserted separately
// because getting this wrong reports every Instagram failure as fetch_failed
// and the cooldown the UI shows would no longer be explained by the code beside
// it.
func TestClassify_SpecificCauseBeatsTheFetchWrapper(t *testing.T) {
	err := fmt.Errorf("%w: %w", ErrFetch, instagram.ErrRateLimited)
	if code, _ := Classify(err); code != FailureRateLimited {
		t.Errorf("code = %q, want %q — ErrFetch swallowed the cause", code, FailureRateLimited)
	}
}

// Every code must have a sentence; a code with none would reach the frontend as
// an empty message field, which is worse than the text it replaced.
func TestFailureMessages_CoverEveryCode(t *testing.T) {
	codes := []FailureCode{
		FailureRateLimited, FailureAuth, FailureSchemaDrift,
		FailureFetch, FailureCancelled, FailureImport,
	}
	for _, code := range codes {
		if failureMessages[code] == "" {
			t.Errorf("no message for code %q", code)
		}
	}
	if len(failureMessages) != len(codes) {
		t.Errorf("failureMessages has %d entries for %d codes — one of them is unreachable",
			len(failureMessages), len(codes))
	}
}
