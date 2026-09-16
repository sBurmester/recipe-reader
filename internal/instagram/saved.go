// internal/instagram/saved.go
package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"

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
	// dropped counts items the feed returned that this code could not read,
	// and drops records why. The endpoint is unofficial and undocumented, so a
	// field rename upstream turns every item into a silent no-op: without
	// these the run reports an ordinary empty success and nothing
	// distinguishes "nothing new was saved" from "the response shape changed".
	dropped := 0
	drops := dropReasons{}
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

		// Named feed, not page: page is the loop's counter, and shadowing it
		// here would silently change what the cursor warning below reports.
		feed, err := decodeFeedPage(res)
		if err != nil {
			// The response did not have the shape of a feed page at all,
			// which is drift of the loudest kind. Reported rather than
			// treated as an empty page, so it cannot read as "nothing new".
			return out, fmt.Errorf("instagram: fetch %s: %w", endpoint, err)
		}
		if len(feed.Items) == 0 {
			break
		}
		for _, raw := range feed.Items {
			post, err := decodeSavedPost(raw)
			if err != nil {
				dropped++
				drops.count(err)
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

		next := feed.NextMaxID
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
		// wrong way to report it. Either way the reasons travel with the
		// count: "no media code" and "cannot decode the item" point at
		// different upstream changes, and guessing between them means reading
		// a raw response by hand.
		if len(out) == 0 {
			return nil, fmt.Errorf("instagram: fetch %s: %w: %d items returned, none readable (%s)",
				endpoint, ErrSchemaDrift, dropped, drops)
		}
		slog.Warn("instagram: some feed items could not be read",
			"endpoint", endpoint, "dropped", dropped, "collected", len(out), "reasons", drops.String())
	}
	return out, nil
}

// ErrSchemaDrift reports that the feed returned items none of which this code
// could read. The saved-posts endpoints are unofficial and undocumented, so
// this is the expected shape of an upstream change — and the one failure mode
// that otherwise arrives as a successful run that imported nothing.
var ErrSchemaDrift = errors.New("instagram: unrecognised response shape")

// The typed shape of a saved-posts feed page.
//
// This used to be `map[string]any` spelunking with the comma-ok form at every
// level. That was panic-free, which is the important part, but it meant a
// field rename or a type change upstream made items vanish with no signal:
// every lookup failed silently, the item was skipped, and the run reported an
// ordinary empty success. Named types put the failure somewhere it can be
// reported — a page that does not decode is ErrSchemaDrift, an item that does
// not decode is a counted drop with a reason.
//
// Items are held as raw JSON rather than decoded with the page, so one
// malformed item drops on its own instead of taking the whole page with it.
// A pointer field is an optional object: nil for absent or null, which is
// ordinary — a video has no image_versions2, a post may carry no caption.
//
// These types are written from the endpoint's documented-by-observation shape,
// not from a verified response: nothing in this repository has yet run against
// a real account (finding I11). Whichever fields turn out to be genuinely
// optional, the drop reasons above are what will say so.
type savedFeedPage struct {
	Items     []json.RawMessage `json:"items"`
	NextMaxID string            `json:"next_max_id"`
}

// savedFeedItem covers both shapes the saved feeds are known to use: the media
// wrapped in a "media" key, and the media object returned directly. The
// embedded mediaObject is the second case, so one decode handles both.
type savedFeedItem struct {
	Media *mediaObject `json:"media"`
	mediaObject
}

type mediaObject struct {
	Code           string         `json:"code"`
	Caption        *captionObject `json:"caption"`
	ImageVersions2 *imageVersions `json:"image_versions2"`
}

type captionObject struct {
	Text string `json:"text"`
}

type imageVersions struct {
	Candidates []imageCandidate `json:"candidates"`
}

type imageCandidate struct {
	URL string `json:"url"`
}

// errNoMediaCode reports an item with no shortcode in it. The code is the
// post's identity and the whole of its permalink, so an item without one
// cannot be imported at all.
var errNoMediaCode = errors.New("no media code")

// decodeFeedPage re-encodes the dependency's map and decodes it into the typed
// page. The round trip is the price of the seam: instago hands back
// map[string]any and exposes no raw body, and one extra marshal per page is
// nothing beside the request that fetched it.
func decodeFeedPage(res map[string]any) (savedFeedPage, error) {
	raw, err := json.Marshal(res)
	if err != nil {
		return savedFeedPage{}, fmt.Errorf("%w: cannot re-encode the response: %w", ErrSchemaDrift, err)
	}
	var page savedFeedPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return savedFeedPage{}, fmt.Errorf("%w: %w", ErrSchemaDrift, err)
	}
	return page, nil
}

// decodeSavedPost turns one raw feed item into a SavedPost, or says why it
// could not.
func decodeSavedPost(raw json.RawMessage) (SavedPost, error) {
	var item savedFeedItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return SavedPost{}, fmt.Errorf("cannot decode the item: %w", err)
	}

	media := item.Media
	if media == nil {
		media = &item.mediaObject
	}
	if media.Code == "" {
		return SavedPost{}, errNoMediaCode
	}

	post := SavedPost{Source: "https://www.instagram.com/p/" + media.Code + "/"}
	if media.Caption != nil {
		post.Caption = media.Caption.Text
	}
	if media.ImageVersions2 != nil && len(media.ImageVersions2.Candidates) > 0 {
		post.ImageURL = media.ImageVersions2.Candidates[0].URL
	}
	return post, nil
}

// dropReasons tallies why items were skipped, so the warning says what changed
// upstream rather than only how many items it cost.
type dropReasons map[string]int

// count records one drop. Decode errors are grouped under one key: their text
// carries the offset of the byte that failed, which would otherwise make every
// item its own reason.
func (d dropReasons) count(err error) {
	switch {
	case errors.Is(err, errNoMediaCode):
		d[errNoMediaCode.Error()]++
	default:
		d["undecodable item"]++
	}
}

// String renders the tally in a stable order, so two runs' logs compare.
func (d dropReasons) String() string {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, d[k]))
	}
	return strings.Join(parts, ", ")
}
