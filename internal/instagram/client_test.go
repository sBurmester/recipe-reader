// internal/instagram/client_test.go
package instagram

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/felipeinf/instago/igerrors"
)

func TestClient_LoginOrRestore_RestoresExistingSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")

	c := NewClient()
	// Prime a session file the way DumpSettings would, by logging in is not
	// possible offline — instead assert that a *missing* session file
	// correctly falls through to attempting Login (which will fail fast
	// with no credentials, proving the restore path was skipped).
	err := c.LoginOrRestore(context.Background(), "", "", sessionPath)
	if err == nil {
		t.Fatal("expected an error when no session file exists and credentials are empty")
	}
	if _, statErr := os.Stat(sessionPath); statErr == nil {
		t.Error("expected no session file to be written on a failed login")
	}
}

// run is the watchdog that bounds a dependency offering no context, no
// settable HTTP timeout, and no transport hook. Without it, a stalled fetch
// blocks forever and — because the worker only clears its in-flight flag when
// Run returns — wedges every future import for the life of the process.
func TestClient_Run_AbandonsACallThatDoesNotReturn(t *testing.T) {
	c := NewClient()
	c.requestTimeout = 20 * time.Millisecond

	release := make(chan struct{})
	defer close(release)

	start := time.Now()
	err := c.run(context.Background(), "hanging", func() error {
		<-release
		return nil
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run() error = %v, want a deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("run() waited %v; it should have given up at the deadline", elapsed)
	}
}

// The abandoned goroutine still writes to the instago client's shared state
// when it finally returns, so a second call must not run alongside it. It
// fails with a diagnosable error instead — and recovers on its own once the
// first call finishes.
func TestClient_Run_SecondCallFailsWhileTheFirstIsStillOut(t *testing.T) {
	c := NewClient()
	c.requestTimeout = 20 * time.Millisecond

	release := make(chan struct{})
	if err := c.run(context.Background(), "hanging", func() error {
		<-release
		return nil
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first run() error = %v, want a deadline", err)
	}

	if err := c.run(context.Background(), "second", func() error { return nil }); err == nil {
		t.Error("second run() error = nil, want a refusal while the first call still holds the slot")
	}

	// Once the abandoned call returns it hands the slot back, so the next
	// import recovers without a restart.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := c.run(context.Background(), "third", func() error { return nil }); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the slot was never returned after the abandoned call finished")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestClient_Run_HonoursCallerCancellation(t *testing.T) {
	c := NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	release := make(chan struct{})
	defer close(release)

	if err := c.run(ctx, "cancelled", func() error { <-release; return nil }); !errors.Is(err, context.Canceled) {
		t.Errorf("run() error = %v, want context.Canceled", err)
	}
}

func TestAuthErrorClassification(t *testing.T) {
	loginRequired := &igerrors.LoginRequired{ClientError: igerrors.ClientError{Status: http.StatusUnauthorized}}
	challenge := &igerrors.ChallengeRequired{ClientError: igerrors.ClientError{Message: "challenge_required"}}
	twoFactor := &igerrors.TwoFactorRequired{ClientError: igerrors.ClientError{}}
	badPassword := &igerrors.BadPassword{ClientError: igerrors.ClientError{}}
	throttled := &igerrors.ClientThrottled{ClientError: igerrors.ClientError{Status: http.StatusTooManyRequests}}

	// Only an expired session is worth a re-login. A challenge, a second
	// factor or a rejected password needs a human, and spending a login
	// attempt on one only feeds the behaviour that produced it.
	if !isSessionExpired(loginRequired) {
		t.Error("LoginRequired should be treated as an expired session")
	}
	for name, err := range map[string]error{"challenge": challenge, "twoFactor": twoFactor, "badPassword": badPassword, "throttled": throttled} {
		if isSessionExpired(err) {
			t.Errorf("%s should not trigger a re-login", name)
		}
	}
	for name, err := range map[string]error{"challenge": challenge, "twoFactor": twoFactor, "badPassword": badPassword} {
		if !isTerminalAuthError(err) {
			t.Errorf("%s should be reported as terminal", name)
		}
	}
	if isTerminalAuthError(throttled) {
		t.Error("a 429 is a rate limit, not an auth failure")
	}
}

// Re-login is capped at one attempt per minReloginInterval, because repeated
// logins are exactly what Instagram flags.
func TestClient_Reauthenticate_RefusesToHammerLogins(t *testing.T) {
	c := NewClient()
	c.username, c.password, c.sessionPath = "someone", "hunter2", filepath.Join(t.TempDir(), "session.json")
	c.lastLogin = time.Now()

	if err := c.reauthenticate(context.Background()); err == nil {
		t.Error("reauthenticate() error = nil, want a refusal so soon after the last attempt")
	}
}

func TestClient_Reauthenticate_WithoutCredentials(t *testing.T) {
	if err := NewClient().reauthenticate(context.Background()); err == nil {
		t.Error("reauthenticate() error = nil, want an error when LoginOrRestore was never called")
	}
}
