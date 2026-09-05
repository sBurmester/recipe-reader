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
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/server

test:
	go test ./...

lint:
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out
	go vet ./...
	golangci-lint run ./...

vuln:
	govulncheck ./...

check: lint vuln test

run:
	go run ./cmd/server

docker:
	docker build -t recipe-reader .
