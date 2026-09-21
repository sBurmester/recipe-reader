// Package logging builds the process-wide slog handler from the --log-level
// and --log-format flags.
//
// Every package in this repo logs through slog's default logger, so there is
// one handler for the process and Configure installs it. That is a deliberate
// global: injecting a *slog.Logger into every constructor would be a larger
// change than the two flags are worth, and slog's default exists for exactly
// this shape of program.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

// NewHandler builds the handler for level and format, writing to w. level is
// one of debug, info, warn, error and format is text or json; kong's enum tags
// reject anything else at parse time, and this rejects it again for callers
// that do not come through the command line.
func NewHandler(w io.Writer, level, format string) (slog.Handler, error) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, fmt.Errorf("logging: unknown level %q", level)
	}

	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case "text":
		return slog.NewTextHandler(w, opts), nil
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("logging: unknown format %q", format)
	}
}

// Configure installs the handler for level and format as slog's default, so
// every package that logs picks it up. Logs go to stderr, where they already
// went: stdout belongs to whatever a command prints for a human to read.
func Configure(level, format string) error {
	h, err := NewHandler(os.Stderr, level, format)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(h))
	return nil
}
