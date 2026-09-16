// internal/instagram/saved_test.go
package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	ig "github.com/felipeinf/instago"
)

// feed builds a stub requester over a fixed list of pages, so the paging loop
// can be driven without a live account. Each page is a slice of post codes;
// the cursor is the page index.
func feed(pages [][]string) (requester, *int) {
	calls := 0
	return func(_ context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
		calls++
		index := 0
		if cursor := opts.Params.Get("max_id"); cursor != "" {
			if _, err := fmt.Sscanf(cursor, "page-%d", &index); err != nil {
				return nil, err
			}
		}
		if index >= len(pages) {
			return map[string]any{"items": []any{}}, nil
		}
		items := make([]any, 0, len(pages[index]))
		for _, code := range pages[index] {
			items = append(items, map[string]any{"media": map[string]any{"code": code}})
		}
		res := map[string]any{"items": items}
		if index+1 < len(pages) {
			res["next_max_id"] = fmt.Sprintf("page-%d", index+1)
		}
		return res, nil
	}, &calls
}

func source(code string) string { return "https://www.instagram.com/p/" + code + "/" }

func codes(posts []SavedPost) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.Source)
	}
	return out
}

// This is the backlog defect. With the newest 50 posts already imported, the
// old loop counted them against maxItems, stopped at the end of the first
// page, and reported a healthy run that imported nothing — leaving everything
// older permanently unreachable. Paging past the known ones is the fix.
func TestPageMedia_PagesPastAlreadyImportedPosts(t *testing.T) {
	req, calls := feed([][]string{{"a", "b"}, {"c", "d"}, {"e", "f"}})
	imported := map[string]bool{source("a"): true, source("b"): true, source("c"): true}

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{
		MaxItems: 2,
		Known:    func(_ context.Context, s string) bool { return imported[s] },
	})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}

	want := []string{source("d"), source("e")}
	if got := codes(posts); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("posts = %v, want the first two unimported ones %v", got, want)
	}
	if *calls < 3 {
		t.Errorf("requests = %d, want at least 3 — the known posts must not end the walk", *calls)
	}
}

// MaxItems still bounds a run: the backlog drains a slice at a time rather
// than in one unbounded fetch.
func TestPageMedia_StopsAtMaxItems(t *testing.T) {
	req, _ := feed([][]string{{"a", "b", "c"}, {"d", "e", "f"}})

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 4})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if len(posts) != 4 {
		t.Errorf("len(posts) = %d, want 4", len(posts))
	}
}

// A feed that keeps handing back a cursor — a server-side loop, or simply an
// account with more history than one run should walk — must not page forever.
func TestPageMedia_StopsAtMaxPages(t *testing.T) {
	// Every page is already imported, so nothing ever fills MaxItems and only
	// the page cap can end the walk.
	pages := make([][]string, 50)
	for i := range pages {
		pages[i] = []string{fmt.Sprintf("code-%d", i)}
	}
	req, calls := feed(pages)

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{
		MaxItems: 100,
		MaxPages: 3,
		Known:    func(context.Context, string) bool { return true },
	})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("len(posts) = %d, want 0", len(posts))
	}
	if *calls != 3 {
		t.Errorf("requests = %d, want exactly MaxPages (3)", *calls)
	}
}

// A request already in flight cannot be interrupted, so between pages is the
// one place cancellation can take effect. Without this check a shutdown waits
// out the entire walk.
func TestPageMedia_StopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req, calls := feed([][]string{{"a"}, {"b"}, {"c"}})

	cancelling := func(c context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
		cancel()
		return req(c, opts)
	}

	if _, err := pageMedia(ctx, cancelling, "feed/saved/posts/", FetchOptions{MaxItems: 10}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pageMedia() error = %v, want context.Canceled", err)
	}
	if *calls != 1 {
		t.Errorf("requests = %d, want 1 — the loop should stop before the next page", *calls)
	}
}

func TestPageMedia_EmptyFeed(t *testing.T) {
	req, _ := feed(nil)
	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("len(posts) = %d, want 0", len(posts))
	}
}

func TestFetchOptions_Defaults(t *testing.T) {
	opts := FetchOptions{}.withDefaults()
	if opts.MaxItems != defaultMaxItems || opts.MaxPages != defaultMaxPages {
		t.Errorf("withDefaults() = %+v, want the documented bounds", opts)
	}
	if opts.isKnown(context.Background(), "anything") {
		t.Error("a nil Known must report nothing as known")
	}
}

func TestDecodeSavedPost(t *testing.T) {
	raw := json.RawMessage(`{
		"media": {
			"code": "Cxyz123",
			"caption": {"text": "Zutaten: 200g Mehl\nZubereitung: Backen."},
			"image_versions2": {"candidates": [{"url": "https://example.com/photo.jpg"}]}
		}
	}`)

	post, err := decodeSavedPost(raw)
	if err != nil {
		t.Fatalf("decodeSavedPost() error = %v", err)
	}
	if post.Source != "https://www.instagram.com/p/Cxyz123/" {
		t.Errorf("Source = %q", post.Source)
	}
	if post.Caption == "" {
		t.Error("expected non-empty caption")
	}
	if post.ImageURL != "https://example.com/photo.jpg" {
		t.Errorf("ImageURL = %q", post.ImageURL)
	}
}

// Some saved endpoints return the media object directly rather than under a
// "media" key. Both shapes decode through the same type.
func TestDecodeSavedPost_UnwrappedMedia(t *testing.T) {
	post, err := decodeSavedPost(json.RawMessage(`{"code":"Cabc","caption":{"text":"hi"}}`))
	if err != nil {
		t.Fatalf("decodeSavedPost() error = %v", err)
	}
	if post.Source != "https://www.instagram.com/p/Cabc/" || post.Caption != "hi" {
		t.Errorf("post = %+v", post)
	}
}

// The optional objects really are optional: a video carries no
// image_versions2, and a post may carry no caption at all.
func TestDecodeSavedPost_OptionalFieldsMayBeAbsentOrNull(t *testing.T) {
	for _, raw := range []string{
		`{"media":{"code":"Cabc"}}`,
		`{"media":{"code":"Cabc","caption":null,"image_versions2":null}}`,
		`{"media":{"code":"Cabc","image_versions2":{"candidates":[]}}}`,
	} {
		post, err := decodeSavedPost(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("decodeSavedPost(%s) error = %v", raw, err)
		}
		if post.Source != "https://www.instagram.com/p/Cabc/" {
			t.Errorf("decodeSavedPost(%s) Source = %q", raw, post.Source)
		}
		if post.Caption != "" || post.ImageURL != "" {
			t.Errorf("decodeSavedPost(%s) invented content: %+v", raw, post)
		}
	}
}

// A drop has to say what was wrong with the item, because "no media code" and
// "undecodable item" point at different upstream changes.
func TestDecodeSavedPost_DropsCarryAReason(t *testing.T) {
	if _, err := decodeSavedPost(json.RawMessage(`{}`)); !errors.Is(err, errNoMediaCode) {
		t.Errorf("error = %v, want errNoMediaCode", err)
	}
	if _, err := decodeSavedPost(json.RawMessage(`{"media":"a string"}`)); err == nil {
		t.Error("a media field of the wrong type decoded without error")
	} else if errors.Is(err, errNoMediaCode) {
		t.Errorf("error = %v, want a decode failure rather than a missing code", err)
	}

	reasons := dropReasons{}
	reasons.count(errNoMediaCode)
	reasons.count(errNoMediaCode)
	reasons.count(errors.New("cannot decode the item: boom"))
	if got, want := reasons.String(), "no media code=2, undecodable item=1"; got != want {
		t.Errorf("dropReasons = %q, want %q", got, want)
	}
}

// A response that is not a feed page at all is the loudest kind of drift, and
// must not read as a page with nothing new on it.
func TestPageMedia_UndecodableResponseIsSchemaDrift(t *testing.T) {
	req := func(context.Context, ig.PrivateRequestOpts) (map[string]any, error) {
		return map[string]any{"items": "not an array"}, nil
	}
	_, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{})
	if !errors.Is(err, ErrSchemaDrift) {
		t.Errorf("error = %v, want ErrSchemaDrift", err)
	}
}

// The saved-posts endpoints are unofficial and undocumented, so a field rename
// upstream turns every item into a silent no-op. Items returned and none of
// them readable is the signature of that, and reporting it as an empty success
// is how it would go unnoticed for as long as nobody wondered why imports
// stopped.
func TestPageMedia_UnreadableFeedIsAnErrorNotAnEmptySuccess(t *testing.T) {
	req := func(context.Context, ig.PrivateRequestOpts) (map[string]any, error) {
		return map[string]any{"items": []any{
			map[string]any{"media": map[string]any{"renamed_code": "abc"}},
			map[string]any{"media": map[string]any{"renamed_code": "def"}},
		}}, nil
	}

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 10})
	if !errors.Is(err, ErrSchemaDrift) {
		t.Fatalf("pageMedia() error = %v, want ErrSchemaDrift", err)
	}
	if posts != nil {
		t.Errorf("posts = %v, want nil", posts)
	}
}

// Some unreadable items alongside readable ones is ordinary — a saved video, a
// deleted post — and must not fail the run.
func TestPageMedia_PartiallyUnreadableFeedStillSucceeds(t *testing.T) {
	req := func(context.Context, ig.PrivateRequestOpts) (map[string]any, error) {
		return map[string]any{"items": []any{
			map[string]any{"media": map[string]any{"renamed_code": "abc"}},
			map[string]any{"media": map[string]any{"code": "good"}},
		}}, nil
	}

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 10})
	if err != nil {
		t.Fatalf("pageMedia() error = %v", err)
	}
	if len(posts) != 1 || posts[0].Source != source("good") {
		t.Errorf("posts = %v, want just the readable one", codes(posts))
	}
}

// A walk throttled part-way through hands back what it collected, so the
// pipeline can import it rather than re-fetching it after the cooldown.
func TestPageMedia_ReturnsCollectedPostsAlongsideAnError(t *testing.T) {
	calls := 0
	req := func(_ context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
		calls++
		if calls > 1 {
			return nil, ErrRateLimited
		}
		return map[string]any{
			"items":       []any{map[string]any{"media": map[string]any{"code": "a"}}},
			"next_max_id": "page-1",
		}, nil
	}

	posts, err := pageMedia(context.Background(), req, "feed/saved/posts/", FetchOptions{MaxItems: 10})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("pageMedia() error = %v, want ErrRateLimited", err)
	}
	if len(posts) != 1 || posts[0].Source != source("a") {
		t.Errorf("posts = %v, want the page collected before the throttle", codes(posts))
	}
}
