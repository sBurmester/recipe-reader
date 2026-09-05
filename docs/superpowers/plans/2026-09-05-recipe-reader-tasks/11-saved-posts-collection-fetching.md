> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 3: Instagram Integration.
>
> **Status:** [ ] not started

# Task 11: Saved-Posts & Collection Fetching

**Files:**
- Create: `internal/instagram/saved.go`
- Test: `internal/instagram/saved_test.go`

**Interfaces:**
- Consumes: `*Client` (Task 10).
- Produces:

```go
type SavedPost struct {
	Source   string // https://www.instagram.com/p/<code>/ — dedupe key for the pipeline
	Caption  string
	ImageURL string
}

type Collection struct {
	ID   string
	Name string
}

func (c *Client) FetchSavedPosts(maxItems int) ([]SavedPost, error)
func (c *Client) ListCollections() ([]Collection, error)
func (c *Client) FetchCollectionPosts(collectionID string, maxItems int) ([]SavedPost, error)
func (c *Client) ResolveCollectionID(name string) (string, error)
```

Task 12 (pipeline) consumes `SavedPost` and calls either `FetchSavedPosts` or `FetchCollectionPosts` (via a small adapter) depending on whether `config.InstagramCollection` is set.

**⚠️ Verification required before production use.** `instago` (verified against its source on 2026-09-05) has no typed method for saved posts or collections — this task calls the private/unofficial `feed/saved/posts/`, `collections/list/`, and `feed/collection/{id}/posts/` endpoints directly via `instago`'s generic `PrivateRequest`, using endpoint paths and a response shape inferred from other open-source Instagram clients, not from `instago`'s own documentation. The media-parsing logic (`extractMedia`) *is* grounded in `instago`'s verified internal JSON field mapping (`caption.text`, `image_versions2.candidates[].url`). Step 6 below is a mandatory manual verification against a real account before this is relied on.

- [ ] **Step 1: Write the failing test (parsing logic only, no network)**

```go
// internal/instagram/saved_test.go
package instagram

import "testing"

func TestExtractMedia(t *testing.T) {
	media := map[string]any{
		"code": "Cxyz123",
		"caption": map[string]any{
			"text": "Zutaten: 200g Mehl\nZubereitung: Backen.",
		},
		"image_versions2": map[string]any{
			"candidates": []any{
				map[string]any{"url": "https://example.com/photo.jpg"},
			},
		},
	}

	post, ok := extractMedia(media)
	if !ok {
		t.Fatal("extractMedia() ok = false, want true")
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

func TestExtractMedia_MissingCode(t *testing.T) {
	if _, ok := extractMedia(map[string]any{}); ok {
		t.Error("expected ok = false when code is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/instagram/... -run TestExtractMedia -v`
Expected: FAIL — `extractMedia` undefined.

- [ ] **Step 3: Implement**

```go
// internal/instagram/saved.go
package instagram

import (
	"fmt"
	"net/url"

	ig "github.com/felipeinf/instago"
)

type SavedPost struct {
	Source   string
	Caption  string
	ImageURL string
}

type Collection struct {
	ID   string
	Name string
}

// FetchSavedPosts pages through the account's "All Posts" saved collection.
// See the Task 11 header note: the endpoint is unofficial and unverified —
// run Step 6 against a real account before depending on this in production.
func (c *Client) FetchSavedPosts(maxItems int) ([]SavedPost, error) {
	return c.pageMedia("feed/saved/posts/", maxItems)
}

func (c *Client) ListCollections() ([]Collection, error) {
	res, err := c.raw.PrivateRequest(ig.PrivateRequestOpts{
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

func (c *Client) ResolveCollectionID(name string) (string, error) {
	collections, err := c.ListCollections()
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

func (c *Client) FetchCollectionPosts(collectionID string, maxItems int) ([]SavedPost, error) {
	return c.pageMedia(fmt.Sprintf("feed/collection/%s/posts/", collectionID), maxItems)
}

func (c *Client) pageMedia(endpoint string, maxItems int) ([]SavedPost, error) {
	var out []SavedPost
	maxID := ""
	for len(out) < maxItems {
		params := url.Values{}
		if maxID != "" {
			params.Set("max_id", maxID)
		}
		res, err := c.raw.PrivateRequest(ig.PrivateRequestOpts{Endpoint: endpoint, Params: params})
		if err != nil {
			return nil, fmt.Errorf("instagram: fetch %s: %w", endpoint, err)
		}
		items, _ := res["items"].([]any)
		if len(items) == 0 {
			break
		}
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			media, ok := item["media"].(map[string]any)
			if !ok {
				media = item // some endpoints return the media object directly
			}
			if post, ok := extractMedia(media); ok {
				out = append(out, post)
			}
		}
		next, _ := res["next_max_id"].(string)
		if next == "" {
			break
		}
		maxID = next
	}
	if len(out) > maxItems {
		out = out[:maxItems]
	}
	return out, nil
}

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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/instagram/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/instagram/saved.go internal/instagram/saved_test.go
git commit -m "$(cat <<'EOF'
feat: add saved-posts and collection fetching via Instagram private API

Endpoint paths are unofficial/reverse-engineered (instago has no typed
method for this); media field parsing is grounded in instago's verified
internal JSON mapping. Needs live verification — see Task 11 Step 6.

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```

- [ ] **Step 6: Manual live verification (do this once, by hand, before relying on the import pipeline)**

```bash
export INSTAGRAM_USERNAME=...
export INSTAGRAM_PASSWORD=...
cat <<'GO' > /tmp/ig_smoke_test.go
package main

import (
	"fmt"
	"os"

	"github.com/sBurmester/recipe-reader/internal/instagram"
)

func main() {
	c := instagram.NewClient()
	if err := c.LoginOrRestore(os.Getenv("INSTAGRAM_USERNAME"), os.Getenv("INSTAGRAM_PASSWORD"), "/tmp/ig-session.json"); err != nil {
		panic(err)
	}
	cols, err := c.ListCollections()
	fmt.Printf("collections: %+v err=%v\n", cols, err)
	posts, err := c.FetchSavedPosts(5)
	fmt.Printf("posts: %+v err=%v\n", posts, err)
}
GO
go run /tmp/ig_smoke_test.go
rm /tmp/ig_smoke_test.go
```

If `collections` or `posts` come back empty despite the account having saved posts, or if `err` is non-nil, inspect the raw response by temporarily logging `res` inside `pageMedia` before the `items` extraction, and adjust `pageMedia`/`extractMedia` field paths to match. This is expected integration work for an unofficial API, not a sign the plan is wrong.


---

[← Task 10](10-instagram-client-wrapper-login-session-persistence.md) · [Task 12 →](12-import-pipeline-fetch-extract-store.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
