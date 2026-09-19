// Package cli is recipe-reader's command line: the kong command tree, the flags
// each command takes, and the dispatch from a parsed command line to the
// package that does the work. No command does that work here.
package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
)

// CLI is the root of the command tree.
//
// serve is the default command and takes its flags without being named
// ("withargs"), so `recipe-reader` and `recipe-reader --http-addr ...` start
// the server exactly as they did before there were commands.
type CLI struct {
	// Version is kong's --version flag: it prints the version Run was given
	// and exits before any command runs. It has no env tag, so a VERSION
	// variable in a .env file cannot trigger it.
	Version kong.VersionFlag `help:"Print the version and exit."`

	Serve       ServeCmd       `cmd:"" default:"withargs" help:"Run the HTTP server and the background import worker. The default command."`
	HealthCheck HealthCheckCmd `cmd:"" name:"healthcheck" help:"Probe the server already listening on HTTP_ADDR; exit 0 if /api/healthz answers ok, 1 otherwise. For container health checks."`
}

// BuildVersion is the version stamped into the binary at link time. It is a
// type of its own so kong can bind it for the Run methods that report it.
type BuildVersion string

// description is the --help preamble. It names the credentials because kong
// cannot: they are not flags, so it has no entry to list them under.
const description = "Imports recipes from Instagram saved posts, extracts structured data, and serves a searchable web UI.\n\n" +
	"Credentials are read from the environment only, never from flags: API_TOKEN (bearer token for writes; " +
	"required on a non-loopback HTTP_ADDR), INSTAGRAM_PASSWORD, LLM_API_KEY, and ANTHROPIC_API_KEY " +
	"(fallback for LLM_API_KEY with the anthropic provider)."

// Main runs the command line and returns the process exit status. SIGINT and
// SIGTERM cancel the context every command runs under.
//
// Every failure — a command line kong rejects or a command that fails — is
// logged as one line and exits 1. kong's own FatalIfErrorf would exit 80 for a
// usage error, and a container HEALTHCHECK understands only 0 and 1.
func Main(args []string, version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, args, version); err != nil {
		slog.Error("fatal", "error", err)
		return 1
	}
	return 0
}

// Run parses args and runs the selected command, with ctx and version bound
// for its Run method. opts are appended to the parser's own; tests use them to
// replace kong's Exit and output writers.
//
// ctx is bound with BindTo because kong matches bindings by concrete type: a
// plain Bind(ctx) would register *signalCtx, and no Run method asks for that.
func Run(ctx context.Context, args []string, version string, opts ...kong.Option) error {
	var root CLI
	parser, err := newParser(&root, version, append([]kong.Option{
		kong.BindTo(ctx, (*context.Context)(nil)),
		kong.Bind(BuildVersion(version)),
	}, opts...)...)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	if err := rejectMisplacedFlags(kctx); err != nil {
		return err
	}
	return kctx.Run()
}

// rejectMisplacedFlags refuses a flag that belongs to a command other than the
// one selected.
//
// serve takes its flags without being named ("withargs"), so kong reads a flag
// in front of another command as serve's and only then switches commands:
// `recipe-reader --http-addr X healthcheck` would probe the default address,
// not X, and `--db-dsn X migrate` would migrate the default database. Both
// parse cleanly, so nothing else would say that the flag went nowhere.
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
			return fmt.Errorf("--%s is not a flag of %s; put flags after the command name", path.Flag.Name, kctx.Command())
		}
	}
	return nil
}

// newParser builds the kong parser for root. It is separate from Run so tests
// can parse a command line without running what it selects.
func newParser(root *CLI, version string, opts ...kong.Option) (*kong.Kong, error) {
	return kong.New(root, append([]kong.Option{
		kong.Name("recipe-reader"),
		kong.Description(description),
		kong.Vars{"version": version},
	}, opts...)...)
}
