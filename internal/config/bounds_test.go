package config

import (
	"strings"
	"testing"
)

// IMPORT_INTERVAL=0 parses cleanly and then panics time.NewTicker on the
// goroutine that calls Worker.Start, which is server.Run's — so the process
// used to abort with a stack trace where every other configuration mistake
// produces one line from slog.Error. A negative one does the same.
func TestLoad_RejectsNonPositiveImportInterval(t *testing.T) {
	for _, interval := range []string{"0", "0s", "-1h"} {
		t.Setenv("IMPORT_INTERVAL", interval)
		_, err := loadArgs(nil)
		if err == nil {
			t.Errorf("loadArgs() with IMPORT_INTERVAL=%s error = nil, want a refusal", interval)
			continue
		}
		if !strings.Contains(err.Error(), "IMPORT_INTERVAL") {
			t.Errorf("loadArgs() with IMPORT_INTERVAL=%s error = %v, want it to name the setting", interval, err)
		}
	}
}

func TestLoad_AcceptsPositiveImportInterval(t *testing.T) {
	t.Setenv("IMPORT_INTERVAL", "30m")
	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.Import.Interval.Minutes() != 30 {
		t.Errorf("Import.Interval = %v, want 30m", cfg.Import.Interval)
	}
}

// Both thresholds are compared against a confidence the extractors only ever
// produce in 0..1, so a value outside that range does not fail — it silently
// makes one branch unreachable. A publish threshold of 80 (meaning a
// percentage) sends every recipe to needs_review; a confidence threshold of 80
// calls the paid LLM for every post, indefinitely.
func TestLoad_RejectsThresholdsOutsideUnitRange(t *testing.T) {
	tests := []struct{ env, value string }{
		{"EXTRACTION_CONFIDENCE_THRESHOLD", "80"},
		{"EXTRACTION_CONFIDENCE_THRESHOLD", "1.0001"},
		{"EXTRACTION_CONFIDENCE_THRESHOLD", "-0.1"},
		{"EXTRACTION_PUBLISH_THRESHOLD", "80"},
		{"EXTRACTION_PUBLISH_THRESHOLD", "-1"},
		{"EXTRACTION_PUBLISH_THRESHOLD", "NaN"},
	}
	for _, tc := range tests {
		t.Run(tc.env+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.env, tc.value)
			err := func() error { _, err := loadArgs(nil); return err }()
			if err == nil {
				t.Fatalf("loadArgs() error = nil, want a refusal")
			}
			if !strings.Contains(err.Error(), tc.env) {
				t.Errorf("loadArgs() error = %v, want it to name %s", err, tc.env)
			}
		})
	}
}

// The endpoints of the range are legal: 0 means "never fall back to the LLM"
// for the confidence threshold, and 1 means "publish nothing unreviewed" for
// the publish one. Both are coherent requests, so neither may be refused.
func TestLoad_AcceptsThresholdEndpoints(t *testing.T) {
	t.Setenv("EXTRACTION_CONFIDENCE_THRESHOLD", "0")
	t.Setenv("EXTRACTION_PUBLISH_THRESHOLD", "1")
	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.Extraction.Threshold != 0 || cfg.Extraction.PublishThreshold != 1 {
		t.Errorf("thresholds = %v / %v, want 0 / 1",
			cfg.Extraction.Threshold, cfg.Extraction.PublishThreshold)
	}
}
