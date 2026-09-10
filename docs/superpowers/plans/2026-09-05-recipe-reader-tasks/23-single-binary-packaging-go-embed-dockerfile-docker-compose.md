> Part of the [Recipe Reader Implementation Plan](../2026-09-05-recipe-reader-implementation.md) — Phase 7: Packaging & CI.
>
> **Status:** [x] done
>
> **Corrections made during implementation:**
> 1. **The final image had no TLS trust store (real bug, fixed).** A bare `alpine` image ships no CA certificates, and the app makes outbound HTTPS calls to both the Instagram and Anthropic APIs — every one would have failed with `x509: certificate signed by unknown authority`. The final stage now installs `ca-certificates`. Verified present in the running container.
> 2. **`INSTAGRAM_SESSION_PATH` was unwritable in the container (real bug, fixed).** It defaults to the *relative* `data/instagram-session.json`, but the final stage set no `WORKDIR`, so it resolved against `/` — which `appuser` (uid 10001) cannot write. Session persistence would have silently failed and the app would have re-logged in on every restart, exactly what [Task 10](10-instagram-client-wrapper-login-session-persistence.md) exists to avoid. Now `WORKDIR /app` with `/app/data` owned by `appuser`, plus an `app-data` volume in compose so it survives restarts. Verified writable in the running container.
> 3. **`Handler()` could panic on an empty request path.** `path[1:]` on an empty `URL.Path` panics, and a panicking file server is not behind the API's recovery middleware. Uses `strings.TrimPrefix` instead, covered by `TestHandler_EmptyPathDoesNotPanic`.
> 4. **The doc comment's rationale was wrong.** The SPA fallback is *not* "required for the hash-router SPA to work on a hard refresh" — hash fragments are never sent to the server, so a deep link arrives as plain `/`. The fallback is still worth having (a typo or stale bookmark returns the app instead of a bare 404), and the comment now says that.
> 5. **Base image bumped:** `alpine:3.20` → `alpine:3.23`, the current release. `node:26-alpine` and `golang:1.27-alpine` were already current (no `node:27` or `golang:1.28` exists yet). `npm install` → `npm ci`, since `package-lock.json` is committed and `ci` installs exactly what it pins.
> 6. **Added `.dockerignore`**, which the task did not mention. Without it `COPY . .` and `COPY web/ ./` drag `.git`, `web/node_modules`, and prior build output into the context — and the committed `internal/webui/dist` placeholder would shadow the frontend stage's real build.
> 7. Added `internal/webui/embed_test.go` (root, fallback, empty-path), which the task did not call for. The tests assert behaviour rather than page content, so they pass with either the placeholder or a real `make frontend` build.
>
> **Verified in the running container:** API and seeded lookups, the real embedded frontend (placeholder absent), hashed assets served with correct content types, SPA fallback returning 200, non-root `uid=10001`, and a 19.5 MB image.

# Task 23: Single-Binary Packaging (go:embed, Dockerfile, docker-compose)

**Files:**
- Create: `internal/webui/embed.go`
- Modify: `cmd/recipe-reader/main.go` (mount the embedded frontend behind the API routes)
- Create: `Dockerfile`, `docker-compose.yml`

**Interfaces:**
- Consumes: `internal/webui/dist/*` (placeholder from Task 1, real build from Task 22's `npm run build` + the `make frontend` copy step).
- Produces: `func webui.Handler() (http.Handler, error)` — SPA-fallback file server. Task 18's `main.go` wiring is extended (not replaced) to serve it for any path not under `/api/`.

- [x] **Step 1: Implement the embed handler**

```go
// internal/webui/embed.go
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the built frontend, falling back to index.html for any
// path that isn't a real file — required for the hash-router SPA to work
// on a hard refresh of a deep link.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			path = path[1:] // fs.Stat wants no leading slash
			if _, err := fs.Stat(sub, path); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
```

- [x] **Step 2: Wire it into `main.go`**

In `run()` (Task 18), replace:

```go
router := api.NewRouter(api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker})
server := &http.Server{Addr: cfg.HTTPAddr, Handler: router}
```

with:

```go
apiRouter := api.NewRouter(api.Deps{Recipes: recipes, Lookups: lookups, Worker: worker})
frontend, err := webui.Handler()
if err != nil {
	return err
}
mux := http.NewServeMux()
mux.Handle("/api/", apiRouter)
mux.Handle("/", frontend)

server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
```

Add `"github.com/sBurmester/recipe-reader/internal/webui"` to the import block.

- [x] **Step 3: Verify with the placeholder frontend**

```bash
go build ./...
make db-up
go run ./cmd/recipe-reader &
curl -s localhost:8080/ | grep -o 'Frontend not built yet'
curl -s localhost:8080/api/healthz
kill %1
```

Expected: the placeholder message and `{"status":"ok"}`.

- [x] **Step 4: Verify with the real frontend**

```bash
make frontend
go run ./cmd/recipe-reader &
curl -s localhost:8080/ | grep -o '<title>Recipe Reader</title>'
curl -s localhost:8080/recipes/999   # SPA fallback: a deep link must still return the app shell, not 404
kill %1
git checkout -- internal/webui/dist/index.html   # restore the committed placeholder for the next `go build` without `make frontend`
```

Expected: both requests return HTML (the real app shell), not the placeholder or a 404.

- [x] **Step 5: Create the `Dockerfile`**

Check `docker --version`, current Node LTS, and current Go stable before building — pin close to what's actually current rather than these exact tags if newer patch releases exist. `sqlc generate`'s output (`internal/db/sqlc/`) is committed to git (Task 3), so the Docker build never needs the `sqlc` CLI itself — it's building already-generated Go source like any other package.

```dockerfile
# syntax=docker/dockerfile:1
FROM node:26-alpine AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o /recipe-reader ./cmd/recipe-reader

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=backend /recipe-reader /usr/local/bin/recipe-reader
USER appuser
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/recipe-reader"]
```

- [x] **Step 6: Create `docker-compose.yml`**

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_DB: recipes
      POSTGRES_USER: recipes
      POSTGRES_PASSWORD: recipes
    volumes:
      - db-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U recipes"]
      interval: 5s
      timeout: 5s
      retries: 5

  app:
    build: .
    depends_on:
      db:
        condition: service_healthy
    environment:
      DB_DSN: "postgres://recipes:recipes@db:5432/recipes?sslmode=disable"
      HTTP_ADDR: ":8080"
      INSTAGRAM_USERNAME: ${INSTAGRAM_USERNAME:-}
      INSTAGRAM_PASSWORD: ${INSTAGRAM_PASSWORD:-}
      INSTAGRAM_COLLECTION: ${INSTAGRAM_COLLECTION:-}
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY:-}
    ports:
      - "8080:8080"

volumes:
  db-data:
```

`make db-up` (Task 1) runs `docker compose up -d db` — the same service the full `app` container talks to, so local dev and the containerized app hit an identical schema/dialect.

- [x] **Step 7: Build and run the Docker image**

```bash
docker compose up --build
curl -s localhost:8080/api/healthz
docker compose down
```

Expected: `{"status":"ok"}`, served by the app container against the compose-managed Postgres.

- [x] **Step 8: Commit**

```bash
git add internal/webui/embed.go cmd/recipe-reader/main.go Dockerfile docker-compose.yml
git commit -m "$(cat <<'EOF'
feat: embed frontend build into the binary and add Docker packaging

Assisted-by: Claude Sonnet 5 via Claude Code
EOF
)"
```


---

[← Task 22](22-import-status-page-router-styling.md) · [Task 24 →](24-ci-workflow-final-documentation.md) · [Back to plan](../2026-09-05-recipe-reader-implementation.md) · [Task index](README.md)
