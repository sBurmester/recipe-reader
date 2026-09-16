package extraction

import (
	"context"
	"errors"
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
