package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/healthcheck"
)

// HealthCheckCmd probes a server that is already running, for the image's
// HEALTHCHECK. It declares the listen address and nothing else: a probe needs
// nothing else, and so cannot be refused over settings it never reads — which
// the --health-check flag it replaces could be, because it shared serve's
// validation.
type HealthCheckCmd struct {
	Addr string `name:"http-addr" env:"HTTP_ADDR" default:"127.0.0.1:8080" help:"Address the server listens on. An unspecified host (\":8080\") is probed on loopback."`
}

// Run probes the server once.
func (c *HealthCheckCmd) Run(ctx context.Context) error {
	return healthcheck.Probe(ctx, c.Addr)
}
