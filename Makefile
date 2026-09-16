.PHONY: build test lint vuln check run docker frontend sqlc-generate db-up

# npm writes this file itself, so it is a real dependency target rather than a
# stamp we invent: `npm ci` reruns only when the lockfile actually changes.
# Without that, making `build` depend on `frontend` would reinstall the whole
# tree on every build.
NODE_MODULES := web/node_modules/.package-lock.json

$(NODE_MODULES): web/package-lock.json
	npm --prefix web ci
	@touch $@

frontend: $(NODE_MODULES)
	npm --prefix web run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -r web/dist/. internal/webui/dist/
	@touch internal/webui/dist/.gitkeep

sqlc-generate:
	sqlc generate

db-up:
	docker compose up -d db

# Depends on frontend: a fresh clone could otherwise `make build`, exit 0, and
# produce a binary serving the placeholder page instead of the application —
# with no warning and no difference in exit code. The binary also says so at
# startup now (webui.IsPlaceholder), for the build paths that bypass this
# Makefile entirely.
build: frontend
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/recipe-reader

# -race matches CI. Without it the race detector only ran after a push, while
# the local gate (`make check`) skipped the one check most likely to catch a
# regression in Worker's locking, the detached import goroutine or the shutdown
# sequence. Sharing one Postgres container per package made it affordable.
test:
	go test -race ./...

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

check: lint vuln test

run: frontend
	go run ./cmd/recipe-reader

docker:
	docker build -t recipe-reader .
