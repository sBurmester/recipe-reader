package instagram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felipeinf/instago/igerrors"
)

// captureLog points the default logger at a buffer for the rest of the test.
// No test in this package calls t.Parallel, which is what makes swapping the
// process-wide logger safe.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// None of the LoginOrRestore tests touch the network. instago refuses an empty
// password with BadCredentials before it builds a request (auth.go:48-50), and
// a session file is only read from disk.

// With no session file, LoginOrRestore falls through to a password login. This
// test used to be called RestoresExistingSession while asserting the opposite
// path, and passed on `err != nil` alone — which any failure satisfies. It now
// checks that the failure is the login's own refusal, that a login attempt was
// recorded, and that nothing claims a session afterwards.
func TestClient_LoginOrRestore_MissingSessionFallsBackToLogin(t *testing.T) {
	log := captureLog(t)
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	c := NewClient()

	err := c.LoginOrRestore(context.Background(), "someone", "", sessionPath)

	if _, ok := errors.AsType[*igerrors.BadCredentials](err); !ok {
		t.Fatalf("LoginOrRestore() error = %v, want the password login's BadCredentials", err)
	}
	if c.lastLogin.IsZero() {
		t.Error("no login attempt was recorded")
	}
	if c.sessionHeld() {
		t.Error("sessionHeld() = true after a refused login")
	}
	if _, statErr := os.Stat(sessionPath); statErr == nil {
		t.Error("a session file was written for a refused login")
	}
	// A first boot is ordinary, so it is noted rather than warned about.
	if !strings.Contains(log.String(), "no saved session yet") || strings.Contains(log.String(), "level=WARN") {
		t.Errorf("want an info line for the missing session and no warning; log:\n%s", log.String())
	}
}

// The restore branch had no coverage at all. A session file DumpSettings wrote
// is loaded, and no login is attempted — with an empty password, an attempt
// would have failed, so success here is the proof.
func TestClient_LoginOrRestore_RestoresSavedSession(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := NewClient().raw.DumpSettings(sessionPath); err != nil {
		t.Fatalf("write a session file: %v", err)
	}
	c := NewClient()

	if err := c.LoginOrRestore(context.Background(), "someone", "", sessionPath); err != nil {
		t.Fatalf("LoginOrRestore() error = %v, want the saved session restored without a login", err)
	}
	if !c.sessionHeld() {
		t.Error("sessionHeld() = false after restoring a session")
	}
	if !c.lastLogin.IsZero() {
		t.Error("a login was attempted although a session was restored")
	}
	// The credentials are kept either way, so an expiry later can re-log in.
	if c.username != "someone" || c.sessionPath != sessionPath {
		t.Errorf("credentials not kept: username %q, sessionPath %q", c.username, c.sessionPath)
	}
}

// go #13: a session file that exists but cannot be read used to be
// indistinguishable from a missing one. It is now a warning naming the file,
// logged before the password login that replaces it.
func TestClient_LoginOrRestore_WarnsAboutAnUnreadableSession(t *testing.T) {
	log := captureLog(t)
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte(`{"uuids": {"uuid": "trunc`), 0o600); err != nil {
		t.Fatalf("write a truncated session file: %v", err)
	}

	if err := NewClient().LoginOrRestore(context.Background(), "someone", "", sessionPath); err == nil {
		t.Fatal("LoginOrRestore() error = nil, want the fallback login refused")
	}

	out := log.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "could not be restored") || !strings.Contains(out, sessionPath) {
		t.Errorf("want a warning naming the unreadable session file; log:\n%s", out)
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
	loginRequired := &igerrors.LoginRequired{Status: http.StatusUnauthorized}
	challenge := &igerrors.ChallengeRequired{Message: "challenge_required"}
	twoFactor := &igerrors.TwoFactorRequired{ClientError: igerrors.ClientError{}}
	badPassword := &igerrors.BadPassword{ClientError: igerrors.ClientError{}}
	throttled := &igerrors.ClientThrottled{Status: http.StatusTooManyRequests}

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
