package main

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
