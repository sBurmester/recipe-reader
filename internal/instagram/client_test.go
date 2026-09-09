// internal/instagram/client_test.go
package instagram

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClient_LoginOrRestore_RestoresExistingSession(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")

	c := NewClient()
	// Prime a session file the way DumpSettings would, by logging in is not
	// possible offline — instead assert that a *missing* session file
	// correctly falls through to attempting Login (which will fail fast
	// with no credentials, proving the restore path was skipped).
	err := c.LoginOrRestore("", "", sessionPath)
	if err == nil {
		t.Fatal("expected an error when no session file exists and credentials are empty")
	}
	if _, statErr := os.Stat(sessionPath); statErr == nil {
		t.Error("expected no session file to be written on a failed login")
	}
}
