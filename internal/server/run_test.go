package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// Run is the composition root, and cancelling its context is how the process
// stops: SIGTERM from Docker, Ctrl-C in a terminal. Against an empty database it
// has to migrate, seed and serve, and once cancelled it has to drain and return
// nil — not an error, and not never. Nothing else reaches the shutdown sequence,
// whose order (server, then import, then pool) is the subtle part.
func TestRun_ServesUntilCancelledThenReturnsNil(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.HTTPAddr = freeLoopbackAddr(t)
	cfg.DBDSN = testdb.NewDatabase(t, "server_run")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, "v0.0.0-test") }()

	deadline := time.Now().Add(30 * time.Second)
	for healthcheck.Probe(context.Background(), cfg.HTTPAddr) != nil {
		select {
		case err := <-done:
			t.Fatalf("Run() returned before it served: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("GET /api/healthz did not answer ok within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v after cancellation, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run() did not return within 15s of cancellation; its shutdown budget is 10s")
	}
}

// freeLoopbackAddr returns a loopback address nothing is listening on. The
// listener is closed before Run binds the port, so another process could take
// it in between; on a test machine that race is theoretical.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}
