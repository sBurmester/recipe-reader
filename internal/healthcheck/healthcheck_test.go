package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/api"
)

// The probe is run against the real API router rather than a stand-in, so the
// contract it checks — 200 and {"status":"ok"} — is the one /api/healthz
// actually keeps.
func TestProbeHealth_PassesAgainstTheRealRouter(t *testing.T) {
	srv := httptest.NewServer(api.NewRouter(api.Deps{Version: "v1.2.3"}, api.Security{}))
	defer srv.Close()

	if err := Probe(context.Background(), srv.Listener.Addr().String()); err != nil {
		t.Errorf("Probe() error = %v, want nil for a healthy server", err)
	}
}

func TestProbeHealth_FailsWhenTheServerIsNotHealthy(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"503": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
		// Something else answering on the port must not pass for this server.
		"not our health endpoint": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hello")) },
		"wrong status":            func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"starting"}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			if err := Probe(context.Background(), srv.Listener.Addr().String()); err == nil {
				t.Error("Probe() error = nil, want a failure")
			}
		})
	}
}

func TestProbeHealth_FailsWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	if err := Probe(context.Background(), addr); err == nil {
		t.Error("Probe() error = nil with nothing listening")
	}
}

// A server that accepts and never answers must fail the probe, not hang it:
// Docker would otherwise kill the probe at its own timeout with no message.
func TestProbeHealth_GivesUpOnAServerThatDoesNotAnswer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Probe(ctx, srv.Listener.Addr().String()); err == nil {
		t.Error("Probe() error = nil for a server that never answered")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Probe() took %v; it should have given up at the deadline", elapsed)
	}
}

// The container listens on ":8080", which is not a dialable address.
func TestDialAddr(t *testing.T) {
	for listen, want := range map[string]string{
		":8080":          "127.0.0.1:8080",
		"0.0.0.0:8080":   "127.0.0.1:8080",
		"[::]:8080":      "127.0.0.1:8080",
		"127.0.0.1:9090": "127.0.0.1:9090",
		"localhost:8080": "localhost:8080",
		"[::1]:8080":     "[::1]:8080",
		"10.0.0.5:8080":  "10.0.0.5:8080",
	} {
		got, err := dialAddr(listen)
		if err != nil || got != want {
			t.Errorf("dialAddr(%q) = (%q, %v), want %q", listen, got, err, want)
		}
	}
	if _, err := dialAddr("no-port"); err == nil {
		t.Error("dialAddr(\"no-port\") error = nil, want a refusal")
	}
}
