package db

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// goose logs every statement of a migration at Info, full SQL included. The
// start-up log is meant to say which migration ran, not replay it, so those
// lines move to Debug — and only those: the per-migration line stays at Info.
func TestStatementsAtDebug(t *testing.T) {
	for _, tc := range []struct {
		name          string
		level         slog.Level
		wantStatement bool
	}{
		{name: "info hides statements", level: slog.LevelInfo, wantStatement: false},
		{name: "debug shows statements", level: slog.LevelDebug, wantStatement: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(statementsAtDebug{slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: tc.level})})

			logger.Info(gooseStatementMsg, "statement", "CREATE TABLE units ()")
			logger.Info("migration completed", "version", 1)

			out := buf.String()
			if got := strings.Contains(out, gooseStatementMsg); got != tc.wantStatement {
				t.Errorf("statement logged = %v, want %v; log:\n%s", got, tc.wantStatement, out)
			}
			if tc.wantStatement && !strings.Contains(out, "level=DEBUG msg=\""+gooseStatementMsg) {
				t.Errorf("statement not logged at Debug; log:\n%s", out)
			}
			if !strings.Contains(out, "level=INFO msg=\"migration completed\"") {
				t.Errorf("per-migration line missing at Info; log:\n%s", out)
			}
		})
	}
}
