package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// HealthCheckCmd probes a server that is already running, for the image's
// HEALTHCHECK. It embeds the listen address and nothing else: a probe needs
// nothing else, and so cannot be refused over settings it never reads — which
// the --health-check flag it replaces could be, because it shared serve's
// validation. Sharing config.Listen with serve keeps the flag, its environment
// variable and its default in one place.
type HealthCheckCmd struct {
	config.Listen `embed:""`
}

// Run probes the server once.
func (c *HealthCheckCmd) Run(ctx context.Context) error {
	return healthcheck.Probe(ctx, c.Addr)
}
