package cli

import (
	"context"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/server"
)

// ImportCmd runs one import and exits. serve does the same every
// IMPORT_INTERVAL; this does it now — before a rollout, after a restore, or
// after fixing a login — without starting the server.
//
// It takes the settings an import reads and no others: the database, the
// account, the extraction rules, the LLM and the limits of one run. The
// schedule is serve's business, and the listen address is nobody's here.
type ImportCmd struct {
	config.Database     `embed:"" group:"Database"`
	config.Instagram    `embed:"" group:"Instagram"`
	config.Extraction   `embed:"" group:"Extraction"`
	config.LLM          `embed:"" group:"LLM"`
	config.ImportLimits `embed:"" group:"Import"`
}

// Run imports once and reports the tally. A run refused by the import lock —
// because a server or another command is importing right now — fails, so the
// operator sees it rather than reading an empty tally as "nothing new".
func (c *ImportCmd) Run(ctx context.Context) error {
	cfg := config.Config{
		Database:   c.Database,
		Instagram:  c.Instagram,
		Extraction: c.Extraction,
		LLM:        c.LLM,
		// Interval stays zero: ImportOnce never schedules anything.
		Import: config.Import{ImportLimits: c.ImportLimits},
	}

	result, err := server.ImportOnce(ctx, cfg)
	if err != nil {
		// A fetch that fails part-way still imports what it already collected,
		// so a failed run can carry a real tally — and "imported nine, then
		// throttled" is a different decision from "did nothing". Only the
		// counts, not the error: main logs that. Seen is zero for the failures
		// that happen before the first post — a refused lock, a failed login, a
		// fetch that returned nothing — and those get no line.
		if result.Seen > 0 {
			slog.Warn("import did not finish",
				"seen", result.Seen, "imported", result.Imported, "skipped", result.Skipped,
				"no_recipe", result.NoRecipe, "failed", result.Failed, "degraded", result.Degraded)
		}
		return err
	}
	slog.Info("import finished",
		"seen", result.Seen, "imported", result.Imported, "skipped", result.Skipped,
		"no_recipe", result.NoRecipe, "failed", result.Failed, "degraded", result.Degraded)
	return nil
}
