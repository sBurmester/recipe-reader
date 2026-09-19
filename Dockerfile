# syntax=docker/dockerfile:1

FROM node:26-alpine AS frontend
WORKDIR /app/web
# npm ci rather than install: package-lock.json is committed, and ci installs
# exactly what it pins instead of re-resolving ranges on every image build.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# VERSION is stamped into the binary, which is what makes a deployed container
# able to identify itself — `--version`, and the version field of
# /api/healthz. It has to be passed in: .dockerignore excludes .git, so there is
# no history in the build context for `git describe` to read. `make docker`
# supplies it; a bare `docker build` leaves it at dev.
ARG VERSION=dev

# sqlc's generated code is committed, so this stage needs no sqlc CLI — it is
# building ordinary Go source. The frontend build lands where go:embed expects.
COPY --from=frontend /app/web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=${VERSION}" -o /recipe-reader ./cmd/recipe-reader

FROM alpine:3.23
# ca-certificates is required, not optional: the app makes outbound HTTPS calls
# to the Instagram and Anthropic APIs, and a bare alpine image ships no trust
# store, so both would fail with "certificate signed by unknown authority".
#
# /app is created writable for appuser because INSTAGRAM_SESSION_PATH defaults
# to the relative path data/instagram-session.json — with no writable working
# directory the session could not be persisted, and Task 10's whole point is to
# avoid re-logging in on every start.
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 appuser \
    && mkdir -p /app/data \
    && chown -R appuser:appuser /app
COPY --from=backend /recipe-reader /usr/local/bin/recipe-reader
USER appuser
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/recipe-reader"]
