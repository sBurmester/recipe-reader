package instagram

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ig "github.com/felipeinf/instago"
	"github.com/felipeinf/instago/igerrors"
)

// failedStartupClient is a client whose LoginOrRestore failed the way a boot
// login does when it cannot succeed. An empty password is refused by instago
// before any request is built, so this needs no network.
func failedStartupClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient()
	// Short, so a regression that sends the request anyway fails in
	// milliseconds instead of waiting out the two-minute watchdog.
	c.requestTimeout = 200 * time.Millisecond
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := c.LoginOrRestore(context.Background(), "someone", "", sessionPath); err == nil {
		t.Fatal("LoginOrRestore() error = nil, want instago's empty-password refusal")
	}
	return c
}

// A login that failed at startup used to disable imports for the life of the
// process. The client now keeps the credentials and logs in before its next
// request — but not within the re-login floor of the failed attempt, which is
// what keeps a run triggered straight after boot from becoming a second login.
func TestClient_Do_WithoutASessionRespectsTheReloginFloor(t *testing.T) {
	c := failedStartupClient(t)

	_, err := c.do(context.Background(), ig.PrivateRequestOpts{Endpoint: "feed/saved/posts/"})
	if !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("do() error = %v, want ErrReauthRequired", err)
	}
	if !strings.Contains(err.Error(), "floor") {
		t.Errorf("do() error = %v, want the re-login floor to be what refused", err)
	}
}

// Past the floor, the next request logs in first rather than going out without
// a session. Here that login is refused again (the password is still empty),
// and the refusal is what comes back — proving the attempt was a login, not
// an unauthenticated request.
func TestClient_Do_WithoutASessionLogsInFirst(t *testing.T) {
	c := failedStartupClient(t)
	c.mu.Lock()
	c.lastLogin = time.Now().Add(-2 * minReloginInterval)
	c.mu.Unlock()

	_, err := c.do(context.Background(), ig.PrivateRequestOpts{Endpoint: "feed/saved/posts/"})
	if !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("do() error = %v, want ErrReauthRequired", err)
	}
	if badCredentials, ok := errors.AsType[*igerrors.BadCredentials](err); !ok {
		t.Errorf("do() error = %v, want the login's BadCredentials underneath", err)
	} else if badCredentials == nil {
		t.Error("BadCredentials is nil")
	}
}

func TestClient_SessionHeldTracksLoginOutcome(t *testing.T) {
	c := failedStartupClient(t)
	if c.sessionHeld() {
		t.Error("sessionHeld() = true after a failed login")
	}
}
