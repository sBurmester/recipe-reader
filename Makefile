.PHONY: build test cover cover-html lint vuln check run docker frontend frontend-check sqlc-generate db-up

# npm writes this file itself, so it is a real dependency target rather than a
# stamp we invent: `npm ci` reruns only when the lockfile actually changes.
# Without that, making `build` depend on `frontend` would reinstall the whole
# tree on every build.
NODE_MODULES := web/node_modules/.package-lock.json

# VERSION is stamped into the binary so a deployed instance can say what it is,
# through `recipe-reader --version` and the version field of /api/healthz. The
# --dirty suffix is deliberate: a build from an unclean tree is not the commit
# it claims to be, and that is exactly what you want to know when the thing in
# front of you is misbehaving. Override it (`make build VERSION=1.2.3`) where
# git is not available, e.g. inside an image build.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

$(NODE_MODULES): web/package-lock.json
	npm --prefix web ci
	@touch $@

frontend: $(NODE_MODULES)
	npm --prefix web run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -r web/dist/. internal/webui/dist/
	@touch internal/webui/dist/.gitkeep

# `go tool`, not a bare `sqlc`: the version lives in go.mod, so this target and
# CI's codegen-drift gate run the same one. They used not to — this called
# whatever sqlc happened to be installed locally while CI ran sqlc@latest, and
# CI fails on any byte of difference in internal/db/sqlc. An upstream release
# that changed generated output therefore reddened every PR at once, and the
# remediation message CI prints ("run make sqlc-generate") could produce a third
# output again. Bumping it is now `go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest`,
# which is a commit you can see and revert.
#
# golangci-lint and govulncheck stay on latest deliberately: a stale
# vulnerability database is the worse failure, and neither produces a committed
# artifact that a version change can silently invalidate.
sqlc-generate:
	go tool sqlc generate

# Publishes Postgres on 127.0.0.1:5432 for a binary running on the host.
# Compose has no default password any more, so this needs one:
#   POSTGRES_PASSWORD=... make db-up
db-up:
	docker compose up -d db

# Depends on frontend: a fresh clone could otherwise `make build`, exit 0, and
# produce a binary serving the placeholder page instead of the application —
# with no warning and no difference in exit code. The binary also says so at
# startup now (webui.IsPlaceholder), for the build paths that bypass this
# Makefile entirely.
build: frontend
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/recipe-reader ./cmd/recipe-reader

# -race matches CI. Without it the race detector only ran after a push, while
# the local gate (`make check`) skipped the one check most likely to catch a
# regression in Worker's locking, the detached import goroutine or the shutdown
# sequence. Sharing one Postgres container per package made it affordable.
#
# Coverage is measured and printed, and nothing is gated on it. A percentage
# target produces tests written to move the number; a per-package line produces
# the same information the panel's manual read of the suite produced, in one
# command. cover.out is left behind for `go tool cover -html=cover.out`.
test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...
	@go tool cover -func=cover.out | tail -n 1

# The twenty least-covered functions. `test` already prints the per-package
# percentages go test reports and the total; this is the ranking that says where
# a test is worth writing next, which is the question D6 asked coverage to
# answer. Still no threshold anywhere: nothing here fails a build.
cover: test
	@go tool cover -func=cover.out | sort -k3 -g | head -n 20

# The HTML report, for reading one package's uncovered lines.
cover-html: test
	go tool cover -html=cover.out

lint:
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out
	go vet ./...
	golangci-lint run ./...

# Run via `go run` rather than a bare `govulncheck`: it needs no global
# install and no GOPATH/bin on PATH, so `make check` works on a fresh clone,
# and it always resolves the current release. This is the same invocation CI
# uses, so the two cannot drift.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# The frontend is checked here because CI checks it: ci.yml's `frontend` job
# runs typecheck and build, so a TypeScript error or a broken Vite build used
# to pass `make check` locally and fail only after the push. The documented
# pre-commit gate now represents the pipeline.
#
# $(NODE_MODULES) is the same lockfile-keyed dependency `frontend` uses, so
# this costs an `npm ci` only when web/package-lock.json actually changed.
frontend-check: $(NODE_MODULES)
	npm --prefix web run typecheck
	npm --prefix web run build

# Ordered cheapest-first: the static checks fail in seconds, the test run
# starts Postgres containers.
check: lint frontend-check vuln test

run: frontend
	go run ./cmd/recipe-reader

# --build-arg VERSION: the image build has no git history to describe itself
# from, so the version is passed in from here, where there is one.
docker:
	docker build --build-arg VERSION=$(VERSION) -t recipe-reader .
