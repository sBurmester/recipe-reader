// internal/instagram/client.go
package instagram

import (
	"fmt"

	ig "github.com/felipeinf/instago"
)

// Client wraps the unofficial instago Instagram client with the subset of
// behaviour the import pipeline needs: a login that transparently reuses a
// persisted session, plus the saved-post / collection fetchers added in
// saved.go.
type Client struct {
	raw *ig.Client
}

// NewClient returns a Client backed by a fresh instago client. Call
// LoginOrRestore before any fetch method.
func NewClient() *Client {
	return &Client{raw: ig.NewClient()}
}

// LoginOrRestore restores a previously saved session if sessionPath exists
// and is valid, otherwise logs in with username/password and persists the
// resulting session to sessionPath for next time (avoids repeated logins,
// which Instagram rate-limits and may flag as suspicious).
func (c *Client) LoginOrRestore(username, password, sessionPath string) error {
	if err := c.raw.LoadSettings(sessionPath, false); err == nil {
		return nil
	}
	if err := c.raw.Login(username, password, ""); err != nil {
		return fmt.Errorf("instagram: login: %w", err)
	}
	if err := c.raw.DumpSettings(sessionPath); err != nil {
		return fmt.Errorf("instagram: save session: %w", err)
	}
	return nil
}
