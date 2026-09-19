package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/server"
)

// ServeCmd runs the server: the HTTP API, the embedded frontend and the
// background import worker. It takes every setting in config.Config, and kong
// validates them only when serve is the command being run.
type ServeCmd struct {
	Config config.Config `embed:""`
}

// Run serves until ctx is cancelled.
func (c *ServeCmd) Run(ctx context.Context, version BuildVersion) error {
	return server.Run(ctx, c.Config, string(version))
}
