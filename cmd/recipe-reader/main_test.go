package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"

	"github.com/sBurmester/recipe-reader/internal/api"
)

// credentialEnvs are the four settings with no flag form. The model cannot
// list them — they are not flags — so they are named here, once, for clearEnv
// and for the --help description test.
var credentialEnvs = []string{"API_TOKEN", "INSTAGRAM_PASSWORD", "LLM_API_KEY", "ANTHROPIC_API_KEY"}

// unreachableDSN points at a port nothing listens on. A test that expects serve
// to fail before it touches the database passes it, so that should serve ever
// get further, it fails at once instead of migrating whatever localhost:5432
// holds and then serving until the test binary times out.
const unreachableDSN = "postgres://recipes:x@127.0.0.1:1/recipes?sslmode=disable&connect_timeout=1"

// clearEnv unsets every environment variable the command line reads, for the
// duration of the test: each flag's env tag, taken from the parser's model so a
// flag added later is covered without anyone remembering it here, and the
// credentials. As in internal/config, t.Setenv registers the restore (and
// guards against t.Parallel), and os.Unsetenv then makes the variables absent
// rather than empty.
func clearEnv(t *testing.T) {
	t.Helper()
	var root CLI
	parser, err := newParser(&root)
	if err != nil {
		t.Fatalf("newParser() error = %v", err)
	}
	names := append([]string(nil), credentialEnvs...)
	err = kong.Visit(parser.Model, func(node kong.Visitable, next kong.Next) error {
		if value, ok := node.(*kong.Value); ok && value.Tag != nil {
			names = append(names, value.Tag.Envs...)
		}
		return next(nil)
	})
	if err != nil {
		t.Fatalf("walk the kong model: %v", err)
	}
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("Unsetenv(%q): %v", name, err)
		}
	}
}

// exitCode is what the test Exit panics with. kong calls Exit after --help and
// --version; left at os.Exit that would end the test binary, and a stub that
// merely returned would let parsing — and then serve — carry on.
type exitCode int

func panicOnExit() kong.Option {
	return kong.Exit(func(code int) { panic(exitCode(code)) })
}

// exitOf runs f and reports the code kong exited with, or exited=false when f
// returned without exiting.
func exitOf(f func()) (code exitCode, exited bool) {
	defer func() {
		if r := recover(); r != nil {
			c, isExit := r.(exitCode)
			if !isExit {
				panic(r)
			}
			code, exited = c, true
		}
	}()
	f()
	return 0, false
}

// parse builds the parser the way run does and parses args without running
// the selected command.
func parse(t *testing.T, args ...string) (*CLI, *kong.Context, error) {
	t.Helper()
	var root CLI
	parser, err := newParser(&root, panicOnExit())
	if err != nil {
		t.Fatalf("newParser() error = %v", err)
	}
	kctx, err := parser.Parse(args)
	return &root, kctx, err
}

// Every deployment that predates the command tree runs the binary with no
// command at all — the image's ENTRYPOINT, compose, `make run`.
func TestParse_NoArgumentsSelectsServe(t *testing.T) {
	clearEnv(t)
	_, kctx, err := parse(t)
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if got := kctx.Command(); got != "serve" {
		t.Errorf("Command() = %q, want serve", got)
	}
}

// ...and passes serve's flags with no command in front of them.
func TestParse_FlagsWithoutACommandReachServe(t *testing.T) {
	clearEnv(t)
	root, kctx, err := parse(t, "--http-addr", "127.0.0.1:7777", "--extraction-mode", "rule")
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if got := kctx.Command(); got != "serve" {
		t.Errorf("Command() = %q, want serve", got)
	}
	if got := root.Serve.Config.HTTP.Addr; got != "127.0.0.1:7777" {
		t.Errorf("HTTP address = %q, want 127.0.0.1:7777", got)
	}
}

// The old --health-check shared serve's validation, so a probe could be
// refused for a token it never reads. healthcheck validates nothing it does not
// read: kong checks only the nodes on the selected command's path.
func TestParse_HealthCheckIgnoresServeValidation(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("IMPORT_MAX_ITEMS", "0")
	t.Setenv("EXTRACTION_MODE", "banana")
	t.Setenv("IMPORT_INTERVAL", "banana")

	root, kctx, err := parse(t, "healthcheck")
	if err != nil {
		t.Fatalf("parse(healthcheck) error = %v, want serve's rules not applied", err)
	}
	if got := kctx.Command(); got != "healthcheck" {
		t.Errorf("Command() = %q, want healthcheck", got)
	}
	if root.HealthCheck.Addr != ":8080" {
		t.Errorf("Addr = %q, want HTTP_ADDR's :8080", root.HealthCheck.Addr)
	}
}

// ...while serve still refuses the environment healthcheck just accepted.
func TestParse_ServeStillValidates(t *testing.T) {
	clearEnv(t)
	t.Setenv("API_TOKEN", "")
	t.Setenv("HTTP_ADDR", ":8080")

	if _, _, err := parse(t); err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
		t.Errorf("parse() error = %v, want serve to refuse a non-loopback bind without API_TOKEN", err)
	}
}

// serve's settings pick up the four credentials in BeforeApply, which kong has
// to run before Validate, or the API_TOKEN rule never sees the token (E4). The
// config tests parse Config as the app itself; serve runs it embedded in a
// command, named or not, and this is the one test that sets the credentials in
// that shape. The non-loopback address fails the parse unless the token
// arrived before Validate.
func TestParse_ServeReadsCredentialsFromTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("API_TOKEN", "tok")
	t.Setenv("INSTAGRAM_PASSWORD", "pw")
	t.Setenv("LLM_API_KEY", "llm-key")
	t.Setenv("ANTHROPIC_API_KEY", "ant-key")

	for _, args := range [][]string{nil, {"serve"}} {
		root, _, err := parse(t, args...)
		if err != nil {
			t.Fatalf("parse(%q) error = %v, want API_TOKEN read before Validate", args, err)
		}
		cfg := root.Serve.Config
		got := []string{
			cfg.HTTP.APIToken, cfg.Instagram.Password, cfg.LLM.APIKey, cfg.LLM.AnthropicAPIKey,
		}
		if want := []string{"tok", "pw", "llm-key", "ant-key"}; !slices.Equal(got, want) {
			t.Errorf("parse(%q) credentials = %q, want %q", args, got, want)
		}
	}
}

// The flag is gone rather than kept as an alias: the image's HEALTHCHECK ships
// with the binary and was switched with it, and a caller still passing the flag
// must fail loudly — not fall through to the default command and start a server.
func TestParse_LegacyHealthCheckFlagIsRefused(t *testing.T) {
	clearEnv(t)
	if _, _, err := parse(t, "--health-check"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("parse(--health-check) error = %v, want an unknown-flag refusal", err)
	}
}

// serve takes its flags without being named, so kong reads a flag in front of
// another command as serve's and only then switches commands. Unguarded,
// `--http-addr X healthcheck` probed the default address instead of X, and
// reported on whatever answered there. It has to be refused instead (E11).
func TestParse_FlagBeforeAnotherCommandIsRefused(t *testing.T) {
	clearEnv(t)
	for _, args := range [][]string{
		{"--http-addr", "127.0.0.1:1", "healthcheck"},
		{"--db-dsn", unreachableDSN, "migrate"},
		{"--db-dsn", unreachableDSN, "import"},
	} {
		_, _, err := parse(t, args...)
		if err == nil || !strings.Contains(err.Error(), "after the command name") {
			t.Errorf("parse(%q) error = %v, want a refusal saying where the flag goes", args, err)
		}
	}
}

// --version prints the stamped version and exits 0 before any command runs —
// including the default one, which would otherwise start a server.
func TestRun_VersionPrintsTheStampedVersionAndExits(t *testing.T) {
	clearEnv(t)
	defer func(v string) { version = v }(version)
	version = "v1.2.3-4-gabc1234"

	var out bytes.Buffer
	code, exited := exitOf(func() {
		_ = run([]string{"--version"}, kong.Writers(&out, &out), panicOnExit())
	})
	if !exited {
		t.Fatal("run(--version) returned; it must exit before any command runs")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != "v1.2.3-4-gabc1234" {
		t.Errorf("printed %q, want the stamped version", got)
	}
}

// Every other setting is reachable through the environment; --version must not
// be, or a VERSION variable in a .env file would make the process print its
// version and exit instead of starting.
func TestParse_VersionIsFlagOnly(t *testing.T) {
	clearEnv(t)
	t.Setenv("VERSION", "1")
	var err error
	if _, exited := exitOf(func() { _, _, err = parse(t) }); exited {
		t.Fatal("VERSION in the environment triggered --version")
	}
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
}

// run reaches serve's Run with ctx and BuildVersion bound. mode=llm without a
// key is the one serve failure that happens before the database is touched, so
// it proves the dispatch without Postgres; a missing binding would fail with a
// kong error instead. unreachableDSN is the safety net should that ever stop
// being true: without it serve would migrate the default database and then
// serve until the test binary times out.
func TestRun_ServeIsDispatchedWithItsBindings(t *testing.T) {
	clearEnv(t)

	args := []string{"--extraction-mode", "llm", "--db-dsn", unreachableDSN}
	err := run(args)
	if err == nil || !strings.Contains(err.Error(), "needs an API key") {
		t.Errorf("run(--extraction-mode llm) error = %v, want serve's missing-key refusal", err)
	}
}

// run dispatches healthcheck to the probe, against the real API router.
func TestRun_HealthCheckProbesARunningServer(t *testing.T) {
	clearEnv(t)
	srv := httptest.NewServer(api.NewRouter(api.Deps{}, api.Security{}))
	defer srv.Close()

	args := []string{"healthcheck", "--http-addr", srv.Listener.Addr().String()}
	if err := run(args); err != nil {
		t.Errorf("run(healthcheck) error = %v, want nil against a healthy server", err)
	}
}

// Every failure exits 1 — a rejected command line as much as a failed command
// — because 0 and 1 are what a container HEALTHCHECK understands. Only main
// turns an error into a status, and it ends the process doing so, so each
// case runs main in a child: this test binary, re-executed with the command
// line in RECIPE_READER_MAIN_ARGS.
func TestMain_ExitStatus(t *testing.T) {
	if args, ok := os.LookupEnv("RECIPE_READER_MAIN_ARGS"); ok {
		os.Args = append([]string{"recipe-reader"}, strings.Fields(args)...)
		main()
		os.Exit(0)
	}
	clearEnv(t)
	srv := httptest.NewServer(api.NewRouter(api.Deps{}, api.Security{}))
	defer srv.Close()

	for _, tc := range []struct {
		args    string
		want    int
		wantLog string
	}{
		{"--does-not-exist", 1, "invalid command line"},
		{"healthcheck --http-addr 127.0.0.1:1", 1, "fatal"},
		{"healthcheck --http-addr " + srv.Listener.Addr().String(), 0, ""},
	} {
		// A child that started serve by mistake would never exit on its own;
		// the deadline turns that into a failure instead of a hung test binary.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMain_ExitStatus$")
		cmd.Env = append(os.Environ(), "RECIPE_READER_MAIN_ARGS="+tc.args)
		code := 0
		out, err := cmd.CombinedOutput()
		cancel()
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run the test binary: %v", err)
		}
		if code != tc.want {
			t.Errorf("recipe-reader %s exited %d, want %d; output:\n%s", tc.args, code, tc.want, out)
		}
		// A command line the binary could not read and a run that went wrong
		// are different problems for whoever reads the log: the first is fixed
		// by reading --help, the second is not.
		if tc.wantLog != "" && !strings.Contains(string(out), tc.wantLog) {
			t.Errorf("recipe-reader %s logged:\n%s\nwant it to contain %q", tc.args, out, tc.wantLog)
		}
	}
}

// With no flag to list them under, --help is the only place an operator finds
// the credential names; they are in the description.
func TestDescription_NamesEveryCredential(t *testing.T) {
	for _, env := range credentialEnvs {
		if !strings.Contains(description, env) {
			t.Errorf("--help description does not mention %s", env)
		}
	}
}

// commandHelp returns what `recipe-reader <command> --help` prints.
func commandHelp(t *testing.T, command string) string {
	t.Helper()
	clearEnv(t)
	var out bytes.Buffer
	_, exited := exitOf(func() {
		var root CLI
		parser, err := newParser(&root, panicOnExit(), kong.Writers(&out, &out))
		if err != nil {
			t.Fatalf("newParser() error = %v", err)
		}
		_, _ = parser.Parse([]string{command, "--help"})
	})
	if !exited {
		t.Fatalf("%s --help did not exit", command)
	}
	return out.String()
}

// serve takes some twenty flags. Grouped by concern, --help reads as the seven
// things an operator configures rather than one alphabetical wall.
func TestHelp_ServeGroupsFlagsByConcern(t *testing.T) {
	help := commandHelp(t, "serve")
	for _, group := range []string{"Logging", "HTTP", "Database", "Instagram", "Extraction", "LLM", "Import"} {
		if !strings.Contains(help, "\n"+group+"\n") {
			t.Errorf("serve --help has no %q group:\n%s", group, help)
		}
	}
}

// serve --help is where the flags are, but kong prints the app description,
// which names the credentials, only at the top level. With no flag entry of
// their own, the credentials are named in the description of the group each
// belongs to instead (E12).
func TestHelp_ServeNamesEveryCredential(t *testing.T) {
	help := commandHelp(t, "serve")
	for _, env := range credentialEnvs {
		if !strings.Contains(help, env) {
			t.Errorf("serve --help does not mention %s:\n%s", env, help)
		}
	}
}

// The log flags sit on the root of the tree, so they are the one kind of flag
// that may precede a command name: rejectMisplacedFlags allows the root's own
// flags everywhere, and an operator typing `recipe-reader --log-level debug
// migrate` is doing the obvious thing.
func TestParse_LogFlagsAreAllowedBeforeTheCommand(t *testing.T) {
	clearEnv(t)

	root, kctx, err := parse(t, "--log-level", "debug", "--log-format", "json", "migrate")
	if err != nil {
		t.Fatalf("parse() error = %v, want the root flags accepted before the command", err)
	}
	if got := kctx.Command(); got != "migrate" {
		t.Errorf("Command() = %q, want migrate", got)
	}
	if root.Logging.Level != "debug" || root.Logging.Format != "json" {
		t.Errorf("Logging = %+v, want debug/json", root.Logging)
	}
}

func TestParse_RejectsAnUnknownLogLevel(t *testing.T) {
	clearEnv(t)

	if _, _, err := parse(t, "--log-level", "banana", "migrate"); err == nil {
		t.Fatal("parse(--log-level banana) = nil error, want the enum to reject it")
	}
}

// The two flags sit on the root of the tree, so the Logging group belongs in
// every command's help, not just serve's. healthcheck is the one that would
// lose it unnoticed: its flag set is deliberately minimal — config.Listen
// exists so that it sees the address and nothing else — so it has no group of
// its own to drag the heading in.
func TestHelp_EveryCommandShowsTheLoggingGroup(t *testing.T) {
	for _, command := range []string{"serve", "healthcheck", "migrate", "import"} {
		t.Run(command, func(t *testing.T) {
			help := commandHelp(t, command)
			if !strings.Contains(help, "\nLogging\n") {
				t.Errorf("%s --help has no Logging group:\n%s", command, help)
			}
			for _, flag := range []string{"--log-level", "--log-format", "$LOG_LEVEL", "$LOG_FORMAT"} {
				if !strings.Contains(help, flag) {
					t.Errorf("%s --help does not name %s:\n%s", command, flag, help)
				}
			}
		})
	}
}

// The one line the two flags exist for is logging.Configure in run: without it
// they parse and change nothing, and the parse tests above would not notice.
// healthcheck against a live test server is the cheapest command line that
// runs to completion, and slog's default afterwards is what run installed.
//
// run replaces that default process-wide and does not restore it, which is
// harmless for every other test in this package (none reads a log line) but
// not for this one's successors, so it is restored here.
func TestRun_InstallsTheLoggerFromTheParsedFlags(t *testing.T) {
	clearEnv(t)
	defer slog.SetDefault(slog.Default())

	srv := httptest.NewServer(api.NewRouter(api.Deps{}, api.Security{}))
	defer srv.Close()

	args := []string{"--log-level", "debug", "--log-format", "json",
		"healthcheck", "--http-addr", srv.Listener.Addr().String()}
	if err := run(args); err != nil {
		t.Fatalf("run(healthcheck) error = %v, want nil against a healthy server", err)
	}

	handler := slog.Default().Handler()
	if !handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("the default logger drops debug records after run(--log-level debug)")
	}
	if _, ok := handler.(*slog.JSONHandler); !ok {
		t.Errorf("the default handler is %T after run(--log-format json), want JSON", handler)
	}
}
