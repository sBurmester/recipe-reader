// Package healthcheck probes a running recipe-reader over HTTP. It is the
// client half of GET /api/healthz, and what the binary's own health check runs,
// so the runtime image needs no curl or wget.
package healthcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// healthCheckTimeout bounds one probe. It sits under the Dockerfile's
// HEALTHCHECK --timeout, so a server that accepts the connection and never
// answers fails the probe with a message rather than being killed by Docker
// with none.
const healthCheckTimeout = 3 * time.Second

// Probe asks the server listening on listenAddr whether it is up, and
// returns nil only when GET /api/healthz answers 200 with {"status":"ok"}.
//
// It is what `recipe-reader --health-check` runs, so the image can carry a
// HEALTHCHECK without a curl or wget in it. The body is checked as well as the
// status so that something else answering on the port does not pass for this
// server.
func Probe(ctx context.Context, listenAddr string) error {
	addr, err := dialAddr(listenAddr)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/healthz", nil)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	// Nothing to do about a failed close of a response already read: the
	// probe's answer is decided by what came before it.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: %s answered %s", req.URL, resp.Status)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&body); err != nil || body.Status != "ok" {
		return fmt.Errorf("health check: %s did not answer {\"status\":\"ok\"}", req.URL)
	}
	return nil
}

// dialAddr turns the address the server listens on into one a probe can dial.
// An unspecified host — ":8080", as the container binds, or "0.0.0.0:8080" or
// "[::]:8080" — listens on every interface, loopback among them, and is not
// itself dialable, so the probe goes to loopback.
func dialAddr(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR %q: %w", listenAddr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}
