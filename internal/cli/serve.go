// Package cli holds recipe-reader's commands: one type per command, whose
// fields are its flags and whose Run hands the work at once to the package
// that does it. cmd/recipe-reader assembles them into the kong command tree,
// parses the command line and runs the selected command.
package cli

import (
	"context"

	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/server"
)

// BuildVersion is the version stamped into the binary at link time. It is a
// type of its own so kong can bind it for the Run methods that report it.
type BuildVersion string

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
