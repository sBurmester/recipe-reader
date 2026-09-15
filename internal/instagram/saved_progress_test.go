package instagram

import (
	"context"
	"slices"
	"testing"

	ig "github.com/felipeinf/instago"
)

// cursorPage is one page of a cursorFeed: the post codes it holds and the
// cursor it names as the next one.
type cursorPage struct {
	codes []string
	next  string
}

// cursorFeed serves pages keyed by the max_id they were requested with, so a
// test can build a feed whose cursors repeat — which feed(), keyed by page
// index, cannot express.
func cursorFeed(pages map[string]cursorPage) (requester, *int) {
	calls := 0
	return func(_ context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
		calls++
		page := pages[opts.Params.Get("max_id")]
		items := make([]any, 0, len(page.codes))
		for _, code := range page.codes {
			items = append(items, map[string]any{"media": map[string]any{"code": code}})
		}
		res := map[string]any{"items": items}
		if page.next != "" {
			res["next_max_id"] = page.next
		}
		return res, nil
	}, &calls
}

func sources(codes ...string) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		out = append(out, source(c))
	}
	return out
}

// The case T-19 names: the cursor does not advance. Without the guard this
// re-requested the "stuck" page until MaxItems filled with copies of c.
func TestPageMedia_StopsWhenTheCursorDoesNotAdvance(t *testing.T) {
	req, calls := cursorFeed(map[string]cursorPage{
		"":      {codes: []string{"a", "b"}, next: "stuck"},
		"stuck": {codes: []string{"c"}, next: "stuck"},
	})

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 50, MaxPages: 100})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if got, want := codes(posts), sources("a", "b", "c"); !slices.Equal(got, want) {
		t.Errorf("posts = %v, want %v", got, want)
	}
	if *calls != 2 {
		t.Errorf("requests = %d, want 2 — the repeated cursor must end the walk", *calls)
	}
}

// A cycle back to an older cursor is the same fault one step removed, and a
// guard that compared only against the last cursor would page through it
// until MaxPages.
func TestPageMedia_StopsOnACursorCycle(t *testing.T) {
	req, calls := cursorFeed(map[string]cursorPage{
		"":   {codes: []string{"a"}, next: "p1"},
		"p1": {codes: []string{"b"}, next: "p2"},
		"p2": {codes: []string{"c"}, next: "p1"},
	})

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 50, MaxPages: 100})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if got, want := codes(posts), sources("a", "b", "c"); !slices.Equal(got, want) {
		t.Errorf("posts = %v, want %v", got, want)
	}
	if *calls != 3 {
		t.Errorf("requests = %d, want 3", *calls)
	}
}

// Overlapping pages are ordinary on a feed that shifts while it is walked —
// a post saved mid-walk pushes the others down a slot. The overlap is
// collected once, not handed to the pipeline twice.
func TestPageMedia_CollectsAPostRepeatedAcrossPagesOnce(t *testing.T) {
	req, _ := cursorFeed(map[string]cursorPage{
		"":   {codes: []string{"a", "b"}, next: "p1"},
		"p1": {codes: []string{"b", "c"}},
	})

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 50})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if got, want := codes(posts), sources("a", "b", "c"); !slices.Equal(got, want) {
		t.Errorf("posts = %v, want %v", got, want)
	}
}
