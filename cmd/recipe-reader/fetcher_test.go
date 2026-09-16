package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

// No account configured: no fetcher, and so no worker — now the only state in
// which the import routes answer 503.
func TestNewFetcher_NoAccountConfigured(t *testing.T) {
	fetcher, err := newFetcher(context.Background(), baseConfig("rule"), instagram.NewClient(), nil)
	if fetcher != nil || err != nil {
		t.Errorf("newFetcher() = (%v, %v), want (nil, nil) without INSTAGRAM_USERNAME", fetcher, err)
	}
}

// integration I8: a login that fails at startup must not withhold the fetcher.
// It used to, which disabled imports for the life of the process — a network
// blip at boot was indistinguishable from having no account configured.
func TestNewFetcher_FailedLoginStillReturnsAFetcher(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.InstagramUsername = "someone" // and no password: instago refuses before any request
	cfg.InstagramSessionPath = filepath.Join(t.TempDir(), "session.json")

	fetcher, err := newFetcher(context.Background(), cfg, instagram.NewClient(), nil)
	if err == nil {
		t.Error("loginErr = nil, want the refused startup login reported")
	}
	if fetcher == nil {
		t.Fatal("fetcher = nil after a failed login; imports would stay disabled until a restart")
	}
}

// integration I6: the per-run bounds reach the fetcher from configuration
// rather than stopping at the package defaults.
func TestNewFetcher_PassesTheConfiguredRunBounds(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.InstagramUsername = "someone"
	cfg.InstagramSessionPath = filepath.Join(t.TempDir(), "session.json")
	cfg.ImportMaxItems, cfg.ImportMaxPages = 500, 400

	fetcher, _ := newFetcher(context.Background(), cfg, instagram.NewClient(), nil)
	pf, ok := fetcher.(*instagram.PipelineFetcher)
	if !ok {
		t.Fatalf("fetcher = %T, want *instagram.PipelineFetcher", fetcher)
	}
	if pf.Options.MaxItems != 500 || pf.Options.MaxPages != 400 {
		t.Errorf("Options = %d items / %d pages, want 500 / 400", pf.Options.MaxItems, pf.Options.MaxPages)
	}
}
