// internal/instagram/saved.go
package instagram

import (
	"fmt"
	"net/url"

	ig "github.com/felipeinf/instago"
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

// FetchSavedPosts pages through the account's "All Posts" saved collection.
// See the Task 11 header note: the endpoint is unofficial and unverified —
// run Step 6 against a real account before depending on this in production.
func (c *Client) FetchSavedPosts(maxItems int) ([]SavedPost, error) {
	return c.pageMedia("feed/saved/posts/", maxItems)
}

// ListCollections returns the account's saved-posts collections (the
// auto "All Posts" collection plus any user-created ones).
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

// ResolveCollectionID looks up a collection's ID by its display name.
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

// FetchCollectionPosts pages through the saved posts in one collection.
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
