.PHONY: build test lint vuln check run docker frontend sqlc-generate db-up

frontend:
	npm --prefix web ci
	npm --prefix web run build
	rm -rf internal/webui/dist
	mkdir -p internal/webui/dist
	cp -r web/dist/. internal/webui/dist/

sqlc-generate:
	sqlc generate

db-up:
	docker compose up -d db

build:
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/recipe-reader

test:
	go test ./...

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

run:
	go run ./cmd/recipe-reader

docker:
	docker build -t recipe-reader .
