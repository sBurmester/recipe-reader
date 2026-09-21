package logging_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"testing"

	"github.com/sBurmester/recipe-reader/internal/logging"
)

func TestNewHandler_JSONWritesOneObjectPerRecord(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "info", "json")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	slog.New(h).Info("hello", "count", 3)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not one JSON object: %v (%q)", err, buf.String())
	}
	if record["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", record["msg"])
	}
	if record["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", record["level"])
	}
}

func TestNewHandler_TextIsTheDefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "info", "text")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	slog.New(h).Info("hello", "count", 3)

	if got := buf.String(); !strings.Contains(got, "msg=hello") || !strings.Contains(got, "count=3") {
		t.Errorf("text output = %q, want it to carry msg and the attribute", got)
	}
}

// The level is the reason the flag exists: below it, a record must not cost
// anything, which is what Enabled reports.
func TestNewHandler_LevelSilencesWhatIsBelowIt(t *testing.T) {
	var buf bytes.Buffer
	h, err := logging.NewHandler(&buf, "warn", "text")
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) = true at level warn, want false")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false at level warn, want true")
	}

	logger := slog.New(h)
	logger.Info("hello")
	if got := buf.String(); got != "" {
		t.Errorf("output after Info at level warn = %q, want empty", got)
	}

	logger.Error("world")
	if got := buf.String(); got == "" {
		t.Error("output after Error at level warn = \"\", want a record")
	}
}

func TestNewHandler_RejectsWhatItCannotBuild(t *testing.T) {
	for _, tc := range []struct{ name, level, format string }{
		{"unknown level", "banana", "text"},
		{"unknown format", "info", "banana"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := logging.NewHandler(&bytes.Buffer{}, tc.level, tc.format); err == nil {
				t.Fatalf("NewHandler(%q, %q) = nil error, want one", tc.level, tc.format)
			}
		})
	}
}
