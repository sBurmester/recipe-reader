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
# sqlc's generated code is committed, so this stage needs no sqlc CLI — it is
# building ordinary Go source. The frontend build lands where go:embed expects.
COPY --from=frontend /app/web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o /recipe-reader ./cmd/recipe-reader

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
