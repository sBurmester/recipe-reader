package server

import (
	"fmt"
	"log/slog"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/extraction"
)

// newExtractor builds the extraction engine for cfg.ExtractionMode.
//
// Each mode now does what its name says. Before, only "hybrid" was ever
// inspected: EXTRACTION_MODE=llm fell through to exactly the same nil-LLM
// hybrid as EXTRACTION_MODE=rule, so asking for the LLM selected the *weakest*
// extractor — on every post of every run, with a paid API key sitting unused,
// and nothing in the logs to contradict the operator. The resulting
// needs_review badge pointed at the captions rather than at the configuration,
// which sent anyone investigating in the wrong direction. The project recorded
// this as an open question in its own Task 18 notes and shipped it anyway.
//
// Hence also the startup log line: the selected mode is now stated, which is
// the cheapest possible contradiction of a wrong assumption.
func newExtractor(cfg config.Config) (extraction.Extractor, error) {
	rules := extraction.NewRuleBasedExtractor()
	settings, hasAPIKey := cfg.LLMSettings()

	newLLM := func() (*extraction.LLMExtractor, error) {
		return extraction.NewLLMExtractor(extraction.LLMConfig{
			Provider: extraction.LLMProvider(settings.Provider),
			APIKey:   settings.APIKey,
			Model:    settings.Model,
			BaseURL:  settings.BaseURL,
			Timeout:  settings.Timeout,
		})
	}

	switch cfg.ExtractionMode {
	case "rule":
		slog.Info("extraction mode selected", "mode", "rule", "llm", "disabled")
		return rules, nil

	case "llm":
		// Fail rather than degrade quietly. An operator who asked for the LLM
		// and configured no key has a broken deployment, not a rules-only one,
		// and saying so at boot costs one run instead of a month of weak
		// extractions nobody attributes to the configuration.
		if !hasAPIKey {
			return nil, fmt.Errorf(
				"extraction mode %q needs an API key: set LLM_API_KEY (or ANTHROPIC_API_KEY for the anthropic provider)",
				cfg.ExtractionMode)
		}
		llm, err := newLLM()
		if err != nil {
			return nil, err
		}
		slog.Info("extraction mode selected", "mode", "llm", "provider", settings.Provider, "model", settings.Model)
		return llm, nil

	case "hybrid":
		if !hasAPIKey {
			// The one degraded mode that is correct to enter without failing:
			// "hybrid" asks for the LLM only where the rules fall short, so
			// rules-only is a coherent answer. It is still said out loud.
			slog.Warn("extraction mode selected", "mode", "hybrid", "llm", "disabled",
				"reason", "no API key configured; running rules-only")
			return extraction.NewHybridExtractor(rules, nil, cfg.ExtractionThreshold), nil
		}
		llm, err := newLLM()
		if err != nil {
			return nil, err
		}
		slog.Info("extraction mode selected", "mode", "hybrid", "provider", settings.Provider,
			"model", settings.Model, "fallback_threshold", cfg.ExtractionThreshold)
		return extraction.NewHybridExtractor(rules, llm, cfg.ExtractionThreshold), nil

	default:
		// Unreachable from the command line — the kong enum rejects it first —
		// but newExtractor is also called with hand-built values in tests, and
		// a silent fallthrough here is the defect this function exists to fix.
		return nil, fmt.Errorf("unknown extraction mode %q (want rule, llm or hybrid)", cfg.ExtractionMode)
	}
}
