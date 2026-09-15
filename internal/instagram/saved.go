// internal/instagram/saved.go
package instagram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	ig "github.com/felipeinf/instago"
)

const (
	// defaultMaxItems caps how many new posts one run collects.
	defaultMaxItems = 50

	// defaultMaxPages bounds how far past the feed head one run walks. With
	// roughly 25 items per page this reaches ~2,500 saved posts, and the
	// dependency's flat 1s pacing makes a full walk cost about a minute and a
	// half — affordable once every IMPORT_INTERVAL, and a hard stop against a
	// feed that pages forever.
	defaultMaxPages = 100
)

// SavedPost is one saved Instagram post reduced to what the import pipeline
// needs. Source is the canonical permalink and doubles as the dedupe key.
type SavedPost struct {
	Source   string
	Caption  string
	ImageURL string
}

// Collection is a named saved-posts collection on the logged-in account.
type Collection struct {
	ID   string
	Name string
}

// FetchOptions bounds one paging run.
type FetchOptions struct {
	// MaxItems caps how many *new* posts a run collects. Posts that Known
	// recognises do not count against it, which is what lets a backlog drain
	// across runs instead of the window staying pinned to the newest MaxItems
	// posts forever.
	MaxItems int

	// MaxPages bounds how many pages a run walks, so an enormous feed — or one
	// that keeps handing back a next cursor — cannot run indefinitely.
	MaxPages int

	// Known reports whether a post has already been imported. A nil Known
	// means nothing is known, which is the behaviour before the backlog fix:
	// every run re-collects the newest MaxItems posts.
	//
	// It returns a bare bool rather than (bool, error) on purpose. The
	// pipeline re-checks authoritatively before importing anything, so the
	// only cost of answering "not known" for a post that is in fact known is
	// one redundant extraction — cheaper than aborting a whole run over a
	// transient lookup failure. The caller decides what to do about the
	// failure, where it has somewhere to log it.
	Known func(ctx context.Context, source string) bool
}

// withDefaults fills in the zero values, so a caller can pass FetchOptions{}
// and get the documented bounds rather than a loop that collects nothing.
func (o FetchOptions) withDefaults() FetchOptions {
	if o.MaxItems <= 0 {
		o.MaxItems = defaultMaxItems
	}
	if o.MaxPages <= 0 {
		o.MaxPages = defaultMaxPages
	}
	return o
}

// isKnown answers for a post, treating an unset Known as "nothing is known".
func (o FetchOptions) isKnown(ctx context.Context, source string) bool {
	return o.Known != nil && o.Known(ctx, source)
}

// requester performs one private-API call. Client.do is the production
// implementation; a test supplies a stub, which is the only way to drive the
// paging loop without a live account.
type requester func(ctx context.Context, opts ig.PrivateRequestOpts) (map[string]any, error)

// FetchSavedPosts pages through the account's "All Posts" saved collection.
// See the Task 11 header note: the endpoint is unofficial and unverified —
// run Step 6 against a real account before depending on this in production.
func (c *Client) FetchSavedPosts(ctx context.Context, opts FetchOptions) ([]SavedPost, error) {
	return pageMedia(ctx, c.do, "feed/saved/posts/", opts)
}

// ListCollections returns the account's saved-posts collections (the
// auto "All Posts" collection plus any user-created ones).
func (c *Client) ListCollections(ctx context.Context) ([]Collection, error) {
	res, err := c.do(ctx, ig.PrivateRequestOpts{
		Endpoint: "collections/list/",
		Params:   url.Values{"collection_types": {`["ALL_MEDIA_AUTO_COLLECTION","MEDIA"]`}},
	})
	if err != nil {
		return nil, fmt.Errorf("instagram: list collections: %w", err)
	}
	items, _ := res["items"].([]any)
	var out []Collection
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["collection_id"].(string)
		name, _ := item["collection_name"].(string)
		if id != "" {
			out = append(out, Collection{ID: id, Name: name})
		}
	}
	return out, nil
}

// ResolveCollectionID looks up a collection's ID by its display name.
func (c *Client) ResolveCollectionID(ctx context.Context, name string) (string, error) {
	collections, err := c.ListCollections(ctx)
	if err != nil {
		return "", err
	}
	for _, col := range collections {
		if col.Name == name {
			return col.ID, nil
		}
	}
	return "", fmt.Errorf("instagram: no saved collection named %q", name)
}

// FetchCollectionPosts pages through the saved posts in one collection.
func (c *Client) FetchCollectionPosts(ctx context.Context, collectionID string, opts FetchOptions) ([]SavedPost, error) {
	return pageMedia(ctx, c.do, fmt.Sprintf("feed/collection/%s/posts/", collectionID), opts)
}

// pageMedia walks the feed from its head, collecting posts opts.Known does not
// already recognise.
//
// Counting only *new* posts against MaxItems is the whole of the backlog fix.
// Before it, the loop stopped after MaxItems items of any kind, so the window
// was permanently anchored to the newest 50 saved posts: an account with 500
// imported the first 50 and then reported `Seen: 50, Skipped: 50, Imported: 0`
// forever, which reads as a healthy run. Skipping past known posts lets the
// backlog drain a page at a time across runs, while MaxPages keeps any single
// run bounded.
func pageMedia(ctx context.Context, req requester, endpoint string, opts FetchOptions) ([]SavedPost, error) {
	opts = opts.withDefaults()

	var out []SavedPost
	// dropped counts items the feed returned that this code could not read.
	// The endpoint is unofficial and undocumented, so a field rename upstream
	// turns every item into a silent no-op: without this counter the run
	// reports an ordinary empty success and nothing distinguishes "nothing new
	// was saved" from "the response shape changed".
	dropped := 0
	maxID := ""
	// cursors and collected are the walk's own record of where it has been:
	// every cursor followed, and every post already taken. See the progress
	// guard at the bottom of the loop.
	cursors := map[string]bool{}
	collected := map[string]bool{}
	for page := 0; len(out) < opts.MaxItems && page < opts.MaxPages; page++ {
		// Checked between pages rather than only at the top: this is the one
		// place cancellation can take effect, since a request already in
		// flight cannot be interrupted (see Client.run).
		if err := ctx.Err(); err != nil {
			return out, fmt.Errorf("instagram: fetch %s: %w", endpoint, err)
		}

		params := url.Values{}
		if maxID != "" {
			params.Set("max_id", maxID)
		}
		res, err := req(ctx, ig.PrivateRequestOpts{Endpoint: endpoint, Params: params})
		if err != nil {
			// The posts collected so far come back alongside the error rather
			// than being thrown away. On a rate limit especially, they are the
			// only work this run will get, and the caller can import them
			// while it waits out the cooldown.
			return out, fmt.Errorf("instagram: fetch %s: %w", endpoint, err)
		}

		items, _ := res["items"].([]any)
		if len(items) == 0 {
			break
		}
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				dropped++
				continue
			}
			media, ok := item["media"].(map[string]any)
			if !ok {
				media = item // some endpoints return the media object directly
			}
			post, ok := extractMedia(media)
			if !ok {
				dropped++
				continue
			}
			// collected is checked first: it is free, where Known is a
			// database lookup.
			if collected[post.Source] || opts.isKnown(ctx, post.Source) {
				continue
			}
			collected[post.Source] = true
			out = append(out, post)
			if len(out) >= opts.MaxItems {
				break
			}
		}

		next, _ := res["next_max_id"].(string)
		if next == "" {
			break
		}
		// The progress guard. Every other exit from this loop is decided by
		// the remote — an empty page, an empty cursor — or by the page cap. A
		// server that hands back a cursor it has already given, the one just
		// sent or an older one, would have the walk re-request the same pages
		// until MaxPages; and since those posts are not in the database yet,
		// Known calls them new every time. Checking every cursor seen, not just
		// the last, also catches a cycle.
		if cursors[next] {
			slog.Warn("instagram: feed repeated a pagination cursor; ending the walk",
				"endpoint", endpoint, "page", page, "collected", len(out))
			break
		}
		cursors[next] = true
		maxID = next
	}

	if dropped > 0 {
		// Unreadable items alongside readable ones is ordinary — a saved video
		// or a deleted post. Unreadable items and *nothing* readable is the
		// signature of a changed response shape, and an empty success is the
		// wrong way to report it.
		if len(out) == 0 {
			return nil, fmt.Errorf("instagram: fetch %s: %w: %d items returned, none readable",
				endpoint, ErrSchemaDrift, dropped)
		}
		slog.Warn("instagram: some feed items could not be read",
			"endpoint", endpoint, "dropped", dropped, "collected", len(out))
	}
	return out, nil
}

// ErrSchemaDrift reports that the feed returned items none of which this code
// could read. The saved-posts endpoints are unofficial and undocumented, so
// this is the expected shape of an upstream change — and the one failure mode
// that otherwise arrives as a successful run that imported nothing.
var ErrSchemaDrift = errors.New("instagram: unrecognised response shape")

func extractMedia(media map[string]any) (SavedPost, bool) {
	code, _ := media["code"].(string)
	if code == "" {
		return SavedPost{}, false
	}
	caption := ""
	if capObj, ok := media["caption"].(map[string]any); ok {
		caption, _ = capObj["text"].(string)
	}
	imageURL := ""
	if imgVersions, ok := media["image_versions2"].(map[string]any); ok {
		if candidates, ok := imgVersions["candidates"].([]any); ok && len(candidates) > 0 {
			if first, ok := candidates[0].(map[string]any); ok {
				imageURL, _ = first["url"].(string)
			}
		}
	}
	return SavedPost{
		Source:   "https://www.instagram.com/p/" + code + "/",
		Caption:  caption,
		ImageURL: imageURL,
	}, true
}
