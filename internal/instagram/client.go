// Package instagram fetches saved posts through the unofficial instago client.
// It owns the parts of that dependency a long-running importer cannot leave to
// chance: a bound on every call, a persisted session that is re-established
// when it expires, a named rate-limit error, and a paging loop that reports a
// response it cannot read instead of returning an empty success.
//
// The endpoints are private and unverified against a live account (review task
// T-43), so the response types in saved.go are written from the observed shape.
package instagram

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	ig "github.com/felipeinf/instago"
	"github.com/felipeinf/instago/igerrors"
)

const (
	// defaultRequestTimeout bounds one call into the dependency.
	//
	// It must stay above 60s. PrivateRequest answers an HTTP 408 by sleeping a
	// full minute and retrying once (instago@v1.0.2/client.go:555-559), and
	// that sleep is uninterruptible — a shorter deadline would fire on every
	// one of those legitimate retries rather than on the hangs it is for.
	defaultRequestTimeout = 2 * time.Minute

	// minReloginInterval is the floor between two login attempts. Repeated
	// logins are the behaviour Instagram flags, so the limit lives here, in
	// the client, rather than as a rule each call site has to remember.
	minReloginInterval = 15 * time.Minute
)

// ErrReauthRequired reports that the Instagram session is no longer usable and
// re-authenticating did not fix it. It is deliberately distinguishable from a
// generic fetch failure: "re-authentication required" is an operator action,
// where a transport error is something to wait out.
var ErrReauthRequired = errors.New("instagram: re-authentication required")

// ErrRateLimited reports that Instagram is throttling this account.
//
// The dependency already distinguishes a 429 from any other 4xx, but nothing
// upstream of it did: a throttle arrived as an ordinary fetch failure, the run
// ended, and the next scheduled run walked straight back into it. Naming it is
// what lets the worker back off instead — and backing off is the whole defence
// an unofficial client has against being flagged.
var ErrRateLimited = errors.New("instagram: rate limited")

// Client wraps the unofficial instago Instagram client with the subset of
// behaviour the import pipeline needs: a login that transparently reuses a
// persisted session and re-establishes an expired one, plus the saved-post /
// collection fetchers added in saved.go.
//
// Every call into the dependency goes through run, which bounds it in time.
// That is not a nicety: instago's HTTP clients are built as `&http.Client{Jar:
// jar}` with no Timeout, so a connection that opens and then stalls blocks
// forever, and one stalled fetch would otherwise wedge the import worker's
// single-flight guard for the life of the process.
type Client struct {
	raw            *ig.Client
	requestTimeout time.Duration

	// send is how the fetch methods reach the API: c.do in production. It is
	// a field so a test can drive ListCollections and the paging loop through
	// a stub, which is the only way to exercise them without a live account.
	send requester

	// slot admits one call into raw at a time. See run for why abandoning a
	// call makes this necessary rather than merely tidy.
	slot chan struct{}

	// mu guards the credentials captured by LoginOrRestore, which
	// reauthenticate replays when a session expires mid-run, and whether the
	// client currently holds a session at all.
	mu          sync.Mutex
	username    string
	password    string
	sessionPath string
	lastLogin   time.Time
	hasSession  bool
}

// NewClient returns a Client backed by a fresh instago client. Call
// LoginOrRestore before any fetch method.
func NewClient() *Client {
	c := &Client{
		raw:            ig.NewClient(),
		requestTimeout: defaultRequestTimeout,
		slot:           make(chan struct{}, 1),
	}
	c.send = c.do
	return c
}

// run executes one call into the instago client under a deadline, and gives up
// waiting when the deadline passes.
//
// There is no better seam. PrivateRequest takes no context, PrivateRequestOpts
// has no ctx field, the *http.Client is unexported, and the only transport
// hook — SetProxy — builds its own transport and needs a proxy to point at. So
// the call runs in a goroutine and this function stops waiting for it: the
// watchdog the panel named as the lesser evil, chosen over a forked dependency
// and over leaving the hang unbounded.
//
// The cost is that an abandoned goroutine outlives its caller and still writes
// to the instago client's shared state when it eventually returns. slot is
// what keeps that from being a data race: one call is admitted at a time, and
// a caller that cannot get the slot fails with a diagnosable error instead of
// racing the abandoned one or blocking behind it forever. If the abandoned
// call ever finishes it hands the slot back, and the next run recovers without
// a restart.
func (c *Client) run(ctx context.Context, what string, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	select {
	case c.slot <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("instagram: %s: an earlier request has not returned: %w", what, ctx.Err())
	}

	// Buffered: an abandoned call must be able to finish and release the slot
	// even though nobody is left to receive its result.
	done := make(chan error, 1)
	go func() {
		err := fn()
		<-c.slot
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("instagram: %s: %w", what, ctx.Err())
	}
}

// do performs one private-API request, logging in first if the client holds no
// session, and re-authenticating once if the session turned out to be stale.
//
// The retry is deliberately not a loop, and a challenge or two-factor prompt
// is reported without attempting a login at all: neither is something a
// password login can clear, and spending an attempt on it only feeds the
// behaviour that produced the challenge.
func (c *Client) do(ctx context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
	// A client whose startup login failed has no session. Sending the request
	// anyway would spend it on finding that out, and would rely on this
	// unofficial endpoint answering login_required to route into the re-login
	// below — which nobody has verified. Logging in first, under the same
	// floor every re-login observes, relies on neither.
	if !c.sessionHeld() {
		if err := c.reauthenticate(ctx); err != nil {
			return nil, fmt.Errorf("%w: no session yet, and logging in failed: %w", ErrReauthRequired, err)
		}
	}

	body, err := c.request(ctx, opts)
	switch {
	case err == nil:
		return body, nil
	case isRateLimited(err):
		return nil, fmt.Errorf("%w: %w", ErrRateLimited, err)
	case isTerminalAuthError(err):
		return nil, fmt.Errorf("%w: %w", ErrReauthRequired, err)
	case !isSessionExpired(err):
		return nil, err
	}

	if reauthErr := c.reauthenticate(ctx); reauthErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrReauthRequired, reauthErr)
	}
	body, err = c.request(ctx, opts)
	switch {
	case err == nil:
		return body, nil
	case isRateLimited(err):
		return nil, fmt.Errorf("%w: %w", ErrRateLimited, err)
	case isSessionExpired(err), isTerminalAuthError(err):
		return nil, fmt.Errorf("%w: %w", ErrReauthRequired, err)
	default:
		return nil, err
	}
}

// request is do without the re-authentication, so that the retry cannot
// recurse into another re-authentication.
func (c *Client) request(ctx context.Context, opts ig.PrivateRequestOpts) (map[string]any, error) {
	var body map[string]any
	err := c.run(ctx, opts.Endpoint, func() (err error) {
		body, err = c.raw.PrivateRequest(opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// LoginOrRestore restores a previously saved session if sessionPath exists
// and is valid, otherwise logs in with username/password and persists the
// resulting session to sessionPath for next time (avoids repeated logins,
// which Instagram rate-limits and may flag as suspicious).
//
// The credentials are kept even when the login fails. A session that expires
// later — sessions do expire, and a password change or an account challenge
// invalidates them immediately — is re-established mid-run from them; and a
// login that fails at startup is retried from them by the next request, instead
// of disabling imports until someone notices and restarts the process.
func (c *Client) LoginOrRestore(ctx context.Context, username, password, sessionPath string) error {
	c.mu.Lock()
	c.username, c.password, c.sessionPath = username, password, sessionPath
	c.mu.Unlock()

	err := c.raw.LoadSettings(sessionPath, false)
	if err == nil {
		c.mu.Lock()
		c.hasSession = true
		c.mu.Unlock()
		return nil
	}
	logSessionLoadFailure(sessionPath, err)
	return c.login(ctx, username, password, sessionPath)
}

// logSessionLoadFailure says why a persisted session was not restored, before
// the password login that replaces it.
//
// The error used to be discarded, so a corrupt or truncated session file looked
// exactly like a missing one: both went quietly to a fresh password login,
// which is the event the file exists to avoid and the one the re-login floor
// rations. A missing file is the ordinary first boot and is only noted; any
// other failure means a session was there and could not be read, and is a
// warning — that is the case someone should look at.
func logSessionLoadFailure(sessionPath string, err error) {
	if errors.Is(err, fs.ErrNotExist) {
		slog.Info("instagram: no saved session yet; logging in with the password", "path", sessionPath)
		return
	}
	slog.Warn("instagram: saved session could not be restored; logging in with the password instead",
		"path", sessionPath, "error", err)
}

// login performs the login and persists the resulting session, recording the
// attempt so reauthenticate can rate-limit itself, and whether it left the
// client holding a session.
func (c *Client) login(ctx context.Context, username, password, sessionPath string) error {
	c.mu.Lock()
	c.lastLogin = time.Now()
	c.mu.Unlock()

	err := c.run(ctx, "login", func() error {
		if err := c.raw.Login(username, password, ""); err != nil {
			return fmt.Errorf("instagram: login: %w", err)
		}
		if err := c.raw.DumpSettings(sessionPath); err != nil {
			return fmt.Errorf("instagram: save session: %w", err)
		}
		return nil
	})

	c.mu.Lock()
	c.hasSession = err == nil
	c.mu.Unlock()
	return err
}

// sessionHeld reports whether the client has a session to send requests with:
// one restored from disk or established by a login. It says nothing about
// whether Instagram still honours it — do finds that out, and re-logs in.
func (c *Client) sessionHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hasSession
}

// reauthenticate re-establishes an expired session from the credentials
// LoginOrRestore captured, at most once every minReloginInterval.
func (c *Client) reauthenticate(ctx context.Context) error {
	c.mu.Lock()
	username, password, sessionPath := c.username, c.password, c.sessionPath
	sinceLastLogin := time.Since(c.lastLogin)
	c.mu.Unlock()

	if username == "" {
		return errors.New("no credentials available: LoginOrRestore was never called with a username")
	}
	if sinceLastLogin < minReloginInterval {
		return fmt.Errorf("last login attempt was %s ago, below the %s floor",
			sinceLastLogin.Round(time.Second), minReloginInterval)
	}
	return c.login(ctx, username, password, sessionPath)
}

// isSessionExpired reports whether err says this session will not fetch
// anything again but a fresh login would. LoginRequired covers both the 401
// and the 403 `login_required` body.
func isSessionExpired(err error) bool {
	var loginRequired *igerrors.LoginRequired
	return errors.As(err, &loginRequired)
}

// isRateLimited reports whether err says Instagram is throttling this account.
// All three cases mean the same thing operationally — stop sending requests for
// a while — even though the API expresses them as a 429, a structured
// rate_limit_error body, and a "Please wait a few minutes" message.
func isRateLimited(err error) bool {
	var (
		throttled  *igerrors.ClientThrottled
		rateLimit  *igerrors.RateLimitError
		pleaseWait *igerrors.PleaseWaitFewMinutes
	)
	return errors.As(err, &throttled) || errors.As(err, &rateLimit) || errors.As(err, &pleaseWait)
}

// isTerminalAuthError reports whether err says the account needs human
// attention — a checkpoint challenge, a second factor, or a password that no
// longer works. Logging in again cannot clear any of them.
func isTerminalAuthError(err error) bool {
	var (
		challenge   *igerrors.ChallengeRequired
		twoFactor   *igerrors.TwoFactorRequired
		badPassword *igerrors.BadPassword
	)
	return errors.As(err, &challenge) || errors.As(err, &twoFactor) || errors.As(err, &badPassword)
}
