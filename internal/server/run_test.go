package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/sBurmester/recipe-reader/internal/db/testdb"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// Run is the composition root, and cancelling its context is how the process
// stops: SIGTERM from Docker, Ctrl-C in a terminal. Against an empty database it
// has to migrate, seed and serve, and once cancelled it has to drain and return
// nil — not an error, and not never. The account it is given cannot log in, so
// the worker exists: the refused login has to reach the import status as an
// authentication failure, which only Run's pipeline.ErrLogin wrap makes it, and
// the import half of the drain runs. The order of the drain (server, then
// import, then pool) is not checked: a Run that returned without waiting for
// it would pass as well.
func TestRun_ServesUntilCancelledThenReturnsNil(t *testing.T) {
	cfg := baseConfig("rule")
	cfg.HTTP.Addr = freeLoopbackAddr(t)
	cfg.Database.DSN = testdb.NewDatabase(t, "server_run")
	// An account with no password: instago refuses the startup login before
	// any request, so the worker exists without the test touching the network.
	// ImportInterval is set because a zero one leaves the schedule unstarted.
	cfg.Instagram.Username = "someone"
	cfg.Instagram.SessionPath = filepath.Join(t.TempDir(), "session.json")
	cfg.Import.Interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, "v0.0.0-test") }()

	deadline := time.Now().Add(30 * time.Second)
	for healthcheck.Probe(context.Background(), cfg.HTTP.Addr) != nil {
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

	resp, err := http.Get("http://" + cfg.HTTP.Addr + "/api/import/status")
	if err != nil {
		t.Fatalf("GET /api/import/status: %v", err)
	}
	var status struct {
		Error string `json:"error"`
	}
	err = json.NewDecoder(resp.Body).Decode(&status)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("decode /api/import/status: %v", err)
	}
	if resp.StatusCode != http.StatusOK || status.Error != "instagram_auth" {
		t.Errorf("GET /api/import/status = %d, error %q; want 200, instagram_auth", resp.StatusCode, status.Error)
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
