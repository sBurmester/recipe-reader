package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// These assert behaviour rather than page content, so they hold whether
// dist/ holds the committed placeholder or a real `make frontend` build.
func serve(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_ServesIndexAtRoot(t *testing.T) {
	rec := serve(t, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected a non-empty index.html")
	}
}

// An unknown path must return the app shell rather than a bare 404, so a
// stale bookmark or a typo still lands on the application.
func TestHandler_FallsBackToIndexForUnknownPath(t *testing.T) {
	root := serve(t, "/")
	rec := serve(t, "/recipes/999")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback to index.html)", rec.Code)
	}
	if rec.Body.String() != root.Body.String() {
		t.Error("fallback response differs from index.html")
	}
}

// An empty URL.Path must not panic: slicing the leading slash off without
// checking would, and a panicking file server takes the process down.
func TestHandler_EmptyPathDoesNotPanic(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = ""
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// A binary built without a frontend must say so. This is the guard that makes
// `go build ./...` — which bypasses the Makefile's frontend prerequisite —
// produce something diagnosable rather than a binary that looks complete and
// serves a placeholder page.
func TestIsPlaceholder_MatchesWhatIsServed(t *testing.T) {
	sub, placeholder, err := frontend()
	if err != nil {
		t.Fatalf("frontend() error = %v", err)
	}
	if placeholder != IsPlaceholder() {
		t.Errorf("IsPlaceholder() = %v, but frontend() reports placeholder = %v", IsPlaceholder(), placeholder)
	}

	// Whichever it is, index.html must exist: that is what Handler serves and
	// what the fallback rewrites to.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Errorf("index.html missing from the served filesystem: %v", err)
	}
}

// The placeholder lives outside dist/ precisely so that a `make build` cannot
// overwrite it and silently turn the warning off.
func TestPlaceholder_IsNotInsideTheBuildOutputDirectory(t *testing.T) {
	if _, err := fs.Stat(placeholderFS, "placeholder/index.html"); err != nil {
		t.Errorf("placeholder/index.html is missing: %v", err)
	}
}

// security S7: the frontend was served with no security headers at all, and
// was framable. Every response carries them — the index, a path that falls
// back to it, and a redirect — because the wrapper sits outside the file
// server rather than in any one branch of it.
func TestHandler_SetsSecurityHeadersOnEveryResponse(t *testing.T) {
	want := map[string]string{
		"Content-Security-Policy": contentSecurityPolicy,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
	}
	for _, path := range []string{"/", "/recipes/999", "/index.html"} {
		rec := serve(t, path)
		for header, value := range want {
			if got := rec.Header().Get(header); got != value {
				t.Errorf("%s: %s = %q, want %q", path, header, got, value)
			}
		}
	}
}

// The policy forbids clickjacking and allows no inline code; weakening either
// by accident is the regression worth failing on.
func TestContentSecurityPolicy_IsStrict(t *testing.T) {
	for _, directive := range []string{"frame-ancestors 'none'", "object-src 'none'", "base-uri 'none'", "default-src 'self'"} {
		if !strings.Contains(contentSecurityPolicy, directive) {
			t.Errorf("policy lacks %q: %s", directive, contentSecurityPolicy)
		}
	}
	for _, loosening := range []string{"'unsafe-inline'", "'unsafe-eval'", "*"} {
		if strings.Contains(contentSecurityPolicy, loosening) {
			t.Errorf("policy contains %s: %s", loosening, contentSecurityPolicy)
		}
	}
}

var (
	scriptTag   = regexp.MustCompile(`(?i)<script\b[^>]*>`)
	scriptSrc   = regexp.MustCompile(`(?i)\ssrc\s*=`)
	inlineStyle = regexp.MustCompile(`(?i)<style\b|\sstyle\s*=`)
	inlineEvent = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
)

// inlineCode returns the first thing in page the policy would block — a script
// element with no src, a style element or attribute, or an inline event
// handler — or "" when there is none.
func inlineCode(page []byte) string {
	for _, tag := range scriptTag.FindAll(page, -1) {
		if !scriptSrc.Match(tag) {
			return string(tag)
		}
	}
	if m := inlineStyle.Find(page); m != nil {
		return string(m)
	}
	return string(inlineEvent.Find(page))
}

// The policy only holds while the page it is served with has no inline code.
// Vite's output has none today; if an inline script, style or handler is ever
// added to web/index.html, this fails here instead of the browser blocking it
// silently. It checks whatever is embedded — the placeholder or a real build.
func TestHandler_IndexHasNothingThePolicyWouldBlock(t *testing.T) {
	sub, _, err := frontend()
	if err != nil {
		t.Fatalf("frontend() error = %v", err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if found := inlineCode(index); found != "" {
		t.Errorf("index.html carries inline code the CSP blocks: %q", found)
	}
}

// The matcher itself, since a regexp that never matches would pass the check
// above on any page.
func TestInlineCode_Matcher(t *testing.T) {
	for _, page := range []string{
		`<script>alert(1)</script>`,
		`<script type="module">import "./x.js"</script>`,
		`<style>body{}</style>`,
		`<div style="color:red">`,
		`<body onload="go()">`,
	} {
		if inlineCode([]byte(page)) == "" {
			t.Errorf("not recognised as inline code: %s", page)
		}
	}
	for _, page := range []string{
		`<script type="module" crossorigin src="/assets/index.js"></script>`,
		`<link rel="stylesheet" href="/assets/index.css">`,
	} {
		if inlineCode([]byte(page)) != "" {
			t.Errorf("recognised as inline code, but is not: %s", page)
		}
	}
}
