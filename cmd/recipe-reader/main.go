// Command recipe-reader imports recipes from Instagram saved posts and serves
// them through a searchable web UI. This file is its command line: the kong
// command tree over the commands in internal/cli, the parsing, and the
// dispatch to the selected command's Run.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"

	"github.com/sBurmester/recipe-reader/internal/cli"
	"github.com/sBurmester/recipe-reader/internal/config"
	"github.com/sBurmester/recipe-reader/internal/logging"
)

// version identifies this build. It is stamped at link time — the Makefile and
// the Dockerfile both pass `-ldflags "-X main.version=..."` from `git describe`
// — and stays "dev" for a plain `go build` or `go run`.
//
// A single binary that cannot say what it is has to be identified by hashing
// it, which is the one thing nobody does at the moment they need the answer.
// So it is surfaced twice: `recipe-reader --version` for whoever has a shell on
// the host, and the `version` field of GET /api/healthz for whoever has only
// the port.
var version = "dev"

// CLI is the root of the command tree.
//
// serve is the default command and takes its flags without being named
// ("withargs"), so `recipe-reader` and `recipe-reader --http-addr ...` start
// the server exactly as they did before there were commands.
type CLI struct {
	// Logging applies to every command, so it sits on the root rather than in
	// any one command's flags.
	Logging config.Logging `embed:"" group:"Logging"`

	// Version is kong's --version flag: it prints the version stamped at link
	// time and exits before any command runs. It has no env tag, so a VERSION
	// variable in a .env file cannot trigger it.
	Version kong.VersionFlag `help:"Print the version and exit."`

	Serve       cli.ServeCmd       `cmd:"" default:"withargs" help:"Run the HTTP server and the background import worker. The default command."`
	HealthCheck cli.HealthCheckCmd `cmd:"" name:"healthcheck" help:"Probe the server already listening on HTTP_ADDR; exit 0 if /api/healthz answers ok, 1 otherwise. For container health checks."`
	Migrate     cli.MigrateCmd     `cmd:"" help:"Apply pending database migrations and seed the lookup tables, then exit. serve does the same at every start."`
}

// Validate is a kong hook on the root of the tree, so every parser newParser
// builds refuses a misplaced flag, whether or not the command then runs.
func (c *CLI) Validate(kctx *kong.Context) error {
	return rejectMisplacedFlags(kctx)
}

// description is the --help preamble. It names the credentials because kong
// cannot: they are not flags, so it has no entry to list them under.
const description = "Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI.\n\n" +
	"Credentials are read from the environment only, never from flags: API_TOKEN (bearer token for writes; " +
	"required on a non-loopback HTTP_ADDR), INSTAGRAM_PASSWORD, LLM_API_KEY, and ANTHROPIC_API_KEY " +
	"(fallback for LLM_API_KEY with the anthropic provider)."

// main runs the command line. Every failure — a command line kong rejects or a
// command that fails — is logged as one line and exits 1. kong's own
// FatalIfErrorf would exit 80 for a usage error, and a container HEALTHCHECK
// understands only 0 and 1.
func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run parses args and runs the selected command. SIGINT and SIGTERM cancel the
// context it runs under, and that context and the version are bound for the
// command's Run method. opts are appended to the parser's own; tests use them
// to replace kong's Exit and output writers.
//
// ctx is bound with BindTo because kong matches bindings by concrete type: a
// plain Bind(ctx) would register *signalCtx, and no Run method asks for that.
func run(args []string, opts ...kong.Option) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var root CLI
	parser, err := newParser(&root, append([]kong.Option{
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(cli.BuildVersion(version)),
	}, opts...)...)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	// After Parse, because the flags that say how to log are only known once
	// they are parsed. A command line kong rejects is therefore still reported
	// in the default format — one line, and then the process is over.
	if err := logging.Configure(root.Logging.Level, root.Logging.Format); err != nil {
		return err
	}
	return kctx.Run()
}

// newParser builds the kong parser for root. It is separate from run so tests
// can parse a command line without running what it selects.
func newParser(root *CLI, opts ...kong.Option) (*kong.Kong, error) {
	return kong.New(root, append([]kong.Option{
		kong.Name("recipe-reader"),
		kong.Description(description),
		kong.Vars{"version": version},
		kong.ExplicitGroups(config.Groups()),
	}, opts...)...)
}

// rejectMisplacedFlags refuses a flag that belongs to a command other than the
// one selected.
//
// serve takes its flags without being named ("withargs"), so kong reads a flag
// in front of another command as serve's and only then switches commands:
// `recipe-reader --http-addr X healthcheck` would probe the default address,
// not X, and `--db-dsn X migrate` would migrate the default database. Both
// parse cleanly, so nothing else would say that the flag went nowhere.
//
// CLI.Validate calls it while kong parses.
func rejectMisplacedFlags(kctx *kong.Context) error {
	onPath := map[*kong.Flag]bool{}
	for node := kctx.Selected(); node != nil; node = node.Parent {
		for _, flag := range node.Flags {
			onPath[flag] = true
		}
	}
	for _, flag := range kctx.Model.Flags {
		onPath[flag] = true
	}
	for _, path := range kctx.Path {
		if path.Flag != nil && !onPath[path.Flag] {
			return fmt.Errorf("--%s was given before the command %s; put flags after the command name", path.Flag.Name, kctx.Command())
		}
	}
	return nil
}
