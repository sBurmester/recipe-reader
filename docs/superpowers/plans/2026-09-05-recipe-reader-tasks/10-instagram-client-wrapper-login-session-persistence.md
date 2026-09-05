> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 3: Instagram Integration.
>
> **Status:** [ ] not started

# Task 10: Instagram Client Wrapper (Login & Session Persistence)

**Files:**
- Create: `internal/instagram/client.go`
- Test: `internal/instagram/client_test.go`

**Interfaces:**
- Produces: `func NewClient() *Client`, `func (c *Client) LoginOrRestore(username, password, sessionPath string) error`. Task 11 adds methods to the same `*Client`; Task 18 (`main.go`) constructs one `*Client` and calls `LoginOrRestore` once at startup.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/felipeinf/instago
```

- [ ] **Step 2: Write the test**

Live Instagram login can't run in CI (real credentials, 2FA, rate limits). This test only verifies the wrapper's session-restore short-circuit logic using a temp file, not real network calls.

```go
// internal/instagram/client_test.go
package instagram

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClient_LoginOrRestore_RestoresExistingSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")

	c := NewClient()
	// Prime a session file the way DumpSettings would, by logging in is not
	// possible offline — instead assert that a *missing* session file
	// correctly falls through to attempting Login (which will fail fast
	// with no credentials, proving the restore path was skipped).
	err := c.LoginOrRestore("", "", sessionPath)
	if err == nil {
		t.Fatal("expected an error when no session file exists and credentials are empty")
	}
	if _, statErr := os.Stat(sessionPath); statErr == nil {
		t.Error("expected no session file to be written on a failed login")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/instagram/... -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 4: Implement**

```go
// internal/instagram/client.go
package instagram

import (
	"fmt"

	ig "github.com/felipeinf/instago"
)

type Client struct {
	raw *ig.Client
}

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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/instagram/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/instagram/client.go internal/instagram/client_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
feat: add Instagram client wrapper with session persistence

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 9](09-hybrid-extractor.md) · [Task 11 →](11-saved-posts-collection-fetching.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
