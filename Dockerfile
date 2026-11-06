# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM node:26-alpine@sha256:8996bb5821d62a8fd9bb27c1cb07462f3fdce6e47d640c6a881ead0b064ce216 AS frontend
WORKDIR /app/web
# npm ci rather than install: package-lock.json is committed, and ci installs
# exactly what it pins instead of re-resolving ranges on every image build.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS backend
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
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /recipe-reader ./cmd/recipe-reader

FROM alpine:3.24@sha256:3f4d6b32e26703c6082839234f43769d5046b3ac7c910033911d1e71d8cd630a
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
# The binary is its own probe: `recipe-reader healthcheck` dials /api/healthz
# on HTTP_ADDR with the container's own environment. This image has no curl or
# wget, and adding one to probe ourselves would grow it for the sake of one GET.
# The probe gives up after 3s, inside --timeout, so a hung server fails it with
# a message rather than being killed by Docker without one. --start-interval
# polls quickly while the server starts, so a healthy container reports so in
# seconds rather than after the first 30s interval.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 \
    CMD ["/usr/local/bin/recipe-reader", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/recipe-reader"]
# serve is the default command either way; naming it makes the default visible,
# and `docker run <image> migrate` or `--version` replaces it as usual.
CMD ["serve"]
