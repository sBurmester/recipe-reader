package extraction

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// blockingClient stands in for a provider that accepts the request and never
// answers — the case E6 describes, and the one the SDKs' retries cannot help
// with because nothing ever comes back to retry on.
type blockingClient struct{}

func (blockingClient) recordRecipe(ctx context.Context, _ string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestLLMExtractor_ExtractIsBoundedByItsTimeout(t *testing.T) {
	e := &LLMExtractor{client: blockingClient{}, timeout: 20 * time.Millisecond}

	// The outer deadline only keeps a regression from hanging the suite; the
	// elapsed check is what tells the two deadlines apart.
	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := e.Extract(parent, "caption")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Extract() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Extract() returned after %v, want the extractor's 20ms timeout to fire, not the test's 5s guard", elapsed)
	}
}

func TestNewLLMExtractor_Timeout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured time.Duration
		want       time.Duration
	}{
		{"unset uses the default", 0, defaultLLMTimeout},
		{"negative uses the default", -time.Second, defaultLLMTimeout},
		{"configured value wins", 3 * time.Minute, 3 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewLLMExtractor(LLMConfig{APIKey: "sk-test", Timeout: tc.configured})
			if err != nil {
				t.Fatalf("NewLLMExtractor() error = %v", err)
			}
			if e.timeout != tc.want {
				t.Errorf("timeout = %v, want %v", e.timeout, tc.want)
			}
		})
	}
}

// A cancelled parent context has to end a real SDK call at once, whatever the
// extractor's own timeout is. The backlog once claimed that an abandoned import
// ran on for up to LLM_TIMEOUT because cancellation "was not threaded into the
// extraction path"; it is: Extract derives its timeout from the caller's
// context, and both SDKs build their requests and wait out their retry backoff
// on it. This runs each SDK against a server that accepts the request and never
// answers, so only the cancellation can end it within the 50ms it is given. The
// extractor's timeout is a backstop at 5s, not an hour: if the cancellation ever
// stopped reaching the call, the test would fail on the deadline error after 5s
// instead of hanging until go test's own timeout.
func TestLLMExtractor_ParentCancellationEndsARealCallAtOnce(t *testing.T) {
	release := make(chan struct{})
	stall := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	// Cleanups run last in, first out: the handler is let go before Close waits
	// for it, or a failing run would hang here instead of failing.
	t.Cleanup(stall.Close)
	t.Cleanup(func() { close(release) })

	for _, provider := range []LLMProvider{ProviderAnthropic, ProviderOpenAI} {
		t.Run(string(provider), func(t *testing.T) {
			e, err := NewLLMExtractor(LLMConfig{
				Provider: provider, APIKey: "sk-test", Model: "test-model",
				BaseURL: stall.URL, Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("NewLLMExtractor() error = %v", err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(50*time.Millisecond, cancel)

			start := time.Now()
			_, err = e.Extract(ctx, "caption")
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Extract() error = %v, want it to wrap context.Canceled", err)
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("Extract() returned after %v, want the cancellation to end it at once", elapsed)
			}
		})
	}
}
