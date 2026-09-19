// Command recipe-reader imports recipes from Instagram saved posts and serves
// them through a searchable web UI. Everything it does lives behind
// internal/cli; this file hands over the command line and the version.
package main

import (
	"os"

	"github.com/sBurmester/recipe-reader/internal/cli"
)

// version identifies this build. It is stamped at link time — the Makefile and
// the Dockerfile both pass `-ldflags "-X main.version=..."` from `git describe`
// — and stays "dev" for a plain `go build` or `go run`. It is surfaced as
// `recipe-reader --version` and as the version field of GET /api/healthz.
var version = "dev"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
