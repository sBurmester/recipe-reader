package pipeline

import (
	"context"
	"errors"
	"testing"
)

// lockStub is an ImportLock whose answer the test chooses. The real lock is
// tested against Postgres in internal/db; what matters here is the branch:
// a refused lock must stop the run before it fetches anything.
type lockStub struct {
	ok       bool
	err      error
	acquired int
	released int
}

func (l *lockStub) TryAcquire(context.Context) (func(), bool, error) {
	l.acquired++
	if l.err != nil {
		return nil, false, l.err
	}
	if !l.ok {
		return nil, false, nil
	}
	return func() { l.released++ }, true, nil
}

func TestRun_RefusedLockStopsBeforeFetching(t *testing.T) {
	lock := &lockStub{ok: false}
	p := &Pipeline{Lock: lock}

	_, err := p.Run(context.Background())

	if !errors.Is(err, ErrImportInProgress) {
		t.Fatalf("Run() error = %v, want ErrImportInProgress", err)
	}
	if lock.acquired != 1 {
		t.Errorf("TryAcquire called %d times, want 1", lock.acquired)
	}
	// A Pipeline with no Fetcher would panic or error the moment it fetched;
	// reaching neither is the proof that the lock came first.
	if lock.released != 0 {
		t.Errorf("release called %d times after a refused lock, want 0", lock.released)
	}
}

func TestRun_LockFailureIsReported(t *testing.T) {
	wantErr := errors.New("boom")
	p := &Pipeline{Lock: &lockStub{err: wantErr}}

	if _, err := p.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, wantErr)
	}
}
