# Code Review — `delivery_reviewer`

**Date:** 2026-09-11
**Reviewer role:** Test and delivery engineer — Go test strategy, CI pipelines, single-binary packaging (panel definition: `agents.yaml` at the repo parent, role `delivery_reviewer`)
**Scope:** all `*_test.go`, `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml`, `internal/db/testdb`, `internal/webui` embedding
**Mode:** single pass, no panel iteration, no consensus round with the other reviewers

My brief treats a missing CI workflow and a missing Dockerfile as release blockers. Both now exist and are good — Tasks 23 and 24 landed. So this review is about what the pipeline does *not* catch and what the test suite costs.

> **Cross-examined by the chair on 2026-09-11.** Ratings below are post-round.
> Findings that moved, merged or were withdrawn carry a note under their heading.
> See the [consensus record](2026-09-11-consensus.md) for the rulings and for
> nine corrections established during the round.

## Rating scale

Each finding carries a rating from **0.1 to 10.0**. The scale is shared by all
six reviewers so the numbers are comparable across reviews, and it weighs three
things together: how likely the defect is to be hit in this project's actual
deployment, how bad the outcome is when it is hit, and how visible the failure
is once it happens — a defect that fails silently rates above an equally severe
one that announces itself.

| Band | Meaning |
| --- | --- |
| 9.0-10.0 | Critical. Exploitable or data-destroying as shipped; fix before the next deploy. |
| 7.0-8.9 | High. A real defect users or operators will hit; schedule it deliberately. |
| 4.0-6.9 | Medium. Genuine, worth fixing, but bounded in blast radius or reach. |
| 2.0-3.9 | Low. Correct to fix; nothing breaks if it waits. |
| 0.1-1.9 | Informational. A note, a nit, or a recorded non-issue. |

Ratings are this reviewer's own, assigned without seeing the other five reviews
and without a chair to reconcile them. Where two reviewers rated the same
underlying defect differently, the index records the divergence rather than
averaging it away.

## Summary

| # | Severity | Rating | Finding | Location |
| --- | --- | --- | --- | --- |
| D1 | High | ~~7.8~~ **7.2** | `make build` produces a binary serving a placeholder UI, silently | `Makefile:17-18`, `internal/webui/dist/index.html` |
| D2 | High | ~~7.2~~ **2.5** | A test makes a real network call to Instagram on every run, CI included | `internal/instagram/client_test.go:48-64` |
| D3 | High | ~~7.0~~ **5.5** | Eight Postgres containers per test run — one per test function, no reuse | 8 call sites of `testdb.New`, no `TestMain` anywhere |
| D4 | Medium | **5.2** | `make test` and CI disagree: no `-race` locally | `Makefile:20-21` vs `.github/workflows/ci.yml:62` |
| D5 | Medium | **6.0** | The riskiest code in the repository is the least tested | `internal/instagram/saved.go`, `internal/extraction/llm.go`, `cmd/recipe-reader/main.go` |
| D6 | Medium | **4.0** | No coverage measurement anywhere — local or CI | `Makefile`, `.github/workflows/ci.yml` |
| D7 | Medium | **5.6** | CI builds an image it never runs, and never publishes one | `.github/workflows/ci.yml:83-90` |
| D8 | Medium | **4.4** | No version stamping — the binary cannot identify itself | `Makefile:17-18`, `Dockerfile:20` |
| D9 | Low | **3.4** | `make check` skips the frontend entirely | `Makefile:32` |
| D10 | Low | **2.2** | `make lint` writes to a fixed `/tmp` path | `Makefile:21` |
| D11 | Low | **3.0** | Unpinned tool versions make CI non-reproducible | `.github/workflows/ci.yml:33,52,55` |
| D12 | Low | **2.6** | The down migration is never executed by any test | `internal/db/migrations/0001_init.down.sql` |
| D13 | Low | **2.8** | No `HEALTHCHECK` in the image, and the `app` compose service has none | `Dockerfile`, `docker-compose.yml:18-36` |

## High

### D1. `make build` produces a binary serving a placeholder UI, silently

**Rating: 7.2 / 10** — revised down from 7.8. Mechanism reproduced from a clean clone: `make build` exits 0 and `strings` finds the placeholder in the binary. Lowered because the served page names the missing step, so it is silent at build time but not to an operator who opens it.

**Location:** `Makefile:3-8,17-18`, `internal/webui/embed.go:4-5,15-16`, `.gitignore:7-8`

```make
build:
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/recipe-reader
```

`build` has no prerequisite on `frontend`. The `//go:embed all:dist` directive needs *something* at `internal/webui/dist`, and the repository commits a placeholder `index.html` to satisfy it — `.gitignore` ignores `internal/webui/dist/*` with an explicit `!index.html` exception. So on a fresh clone, `make build` succeeds, produces a binary, and that binary serves the placeholder page instead of the application. No warning, no failure, no difference in exit code.

This is the packaging trap I look for: the single-binary story's whole premise is that the artifact is complete, and the default path produces an incomplete one that looks identical. The Dockerfile gets this right — the `frontend` stage builds the real bundle and `COPY --from=frontend` overwrites the directory (`Dockerfile:19`), with `.dockerignore` excluding `internal/webui/dist` so the placeholder cannot shadow it, and a comment explaining exactly that. All the correct thinking is in the container path and none of it is in the Make path.

**Fix:** make the dependency explicit and the failure loud.

```make
build: frontend
	CGO_ENABLED=0 go build -o bin/recipe-reader ./cmd/recipe-reader
```

If a full `npm ci` on every build is too slow, keep them separate but add a build-time guard — a marker file the real Vite build emits, checked by a `//go:embed` of that name, or a startup `slog.Warn` when the served `index.html` matches the placeholder's hash. `webui.Handler()` already returns an error; a placeholder build could be made to say so.

### D2. A test makes a real network call to Instagram on every run

**Rating: 2.5 / 10** — chair ruling R3. **The premise below is false and withdrawn:** `instago@v1.0.2/auth.go:49` returns `BadCredentials` before the first network call, verified three ways including a dead-proxy run. No test here has ever contacted Instagram. Note both this review and `integration` I12 cited `client_test.go:48-64` in a 26-line file.

**Location:** `internal/instagram/client_test.go:48-64`

```go
func TestClient_LoginOrRestore_RestoresExistingSession(t *testing.T) {
	...
	err := c.LoginOrRestore("", "", sessionPath)
	if err == nil {
		t.Fatal("expected an error when no session file exists and credentials are empty")
	}
```

With no session file, `LoginOrRestore` falls through to `c.raw.Login("", "", "")` — an actual HTTPS request to Instagram's private API. This runs on every `go test ./...` and on every CI job (`.github/workflows/ci.yml:61-62`), from a shared GitHub runner IP.

Three separate problems: the suite is not hermetic, so a network outage or an Instagram-side change flips the result; the test passes for the wrong reason offline (any error satisfies `err == nil` failing); and CI sends unauthenticated login traffic to the third party whose rate limits this project is otherwise careful about — `integration_reviewer` I12 covers that angle.

The name compounds it: `RestoresExistingSession` asserts the *missing*-session path, as the comment concedes. A reader scanning test names for session-restore coverage will believe it exists. It does not — the restore branch (`client.go:29-31`) is never executed by any test.

**Fix:** make the dependency injectable — a small interface over the two methods `Client` actually uses (`LoadSettings`, `Login`, `DumpSettings`) with a fake in the test — which lets you cover both branches hermetically and finally test the restore path. Failing that, write a valid settings file into `t.TempDir()` first so `LoadSettings` succeeds and the login is never reached, and rename the test to match what it asserts. Nothing in the suite should contact Instagram.

### D3. Eight Postgres containers per test run

**Rating: 5.5 / 10** — revised down from 7.0 on category (it fails loudly, hits only developers, and stays inside CI's 30-minute timeout), while the count was corrected *upward*: **18 containers, not 8** — five call sites sit inside `t.Helper()` wrappers that several tests each invoke.

**Location:** `internal/db/testdb/testdb.go:21-57`; call sites at `db_test.go:12`, `lookup_repository_test.go:13`, `recipe_repository_test.go:14,57,105,143`, `handlers_recipes_test.go:18`, `pipeline_test.go:38`

`testdb.New(t)` starts a fresh container, migrates it, and registers cleanup — per call. There is no `TestMain` anywhere in the repository (verified: `grep -rn "func TestMain"` returns nothing), so nothing is shared. `recipe_repository_test.go` alone starts four; the suite starts eight, each paying container startup (~1-2s), the `BasicWaitStrategies` double readiness gate, a full `migrate.Up`, and a pool connect. In CI that is on the critical path of every push, with `-race` on top, and the workflow's own comment already identifies this job as "the slowest by a wide margin."

The cost is not just minutes. It is the reason nobody will add the integration tests D5 asks for, because each one costs another container.

**Fix:** one container per package, shared across its tests, with isolation from truncation rather than from a new container:

```go
func TestMain(m *testing.M) {
	// start container + migrate once, set a package-level pool, run, terminate
}

func reset(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE recipes, recipe_ingredients, recipe_categories,
		 ingredients, categories, units RESTART IDENTITY CASCADE`)
	...
}
```

Each test calls `reset` and then seeds what it needs. That takes eight containers to four (one per package with database tests) and makes per-test cost a truncation instead of a container boot. `testcontainers-go`'s reuse support can take it further, but `TestMain` alone captures most of the win with no extra machinery.

## Medium

### D4. `make test` and CI disagree

**Rating: 5.2 / 10**

**Location:** `Makefile:20-21`, `.github/workflows/ci.yml:62`

CI runs `go test ./... -race`; `make test` — and therefore `make check`, the documented pre-commit gate — runs `go test ./...` without it. So the race detector only ever runs after a push. Given that this codebase has real concurrency worth checking (`Worker`'s mutex and `running` flag, the detached `go RunOnce` goroutine, the shutdown goroutine in `main`), the local gate is missing the check most likely to catch a regression in it.

**Fix:** add `-race` to the `test` target. It roughly doubles test wall time, which is a real cost — but D3's container startup dominates that number today, so fixing D3 first makes this nearly free.

### D5. The riskiest code in the repository is the least tested

**Rating: 6.0 / 10**

Coverage numbers lie; untested branches bite. Ranked by risk × absence:

1. **`internal/instagram/saved.go` — `pageMedia` (`:77-116`) is entirely untested.** It is the pagination loop against an unofficial, explicitly unverified endpoint: cursor handling, the `media = item` fallback, the `maxItems` truncation, the termination conditions. `saved_test.go` covers only `extractMedia`, the pure function, in two cases. Every genuinely risky line in this package is uncovered, and `integration_reviewer` I1/I7 identify real defects in exactly those lines.
2. **`internal/extraction/llm.go` — `Extract` (`:73-98`) is untested.** `llm_test.go` covers `parseToolInput` in both branches and stops there. The tool-block scan, the `AsAny()` type switch, and the "no tool_use block in response" error path have never run. The constructor makes this hard to fix (`extraction_reviewer` E7) — which is why it is untested, and why fixing E7 is the unlock.
3. **`cmd/recipe-reader/main.go` — 0%.** `run()` is 90 lines of inline wiring with no seam: config, migrate, connect, seed, extractor selection, Instagram login, worker, router, embed, server, shutdown. The extractor-selection branch (`:61`) has a known defect nobody would catch (`go_reviewer` #6, `--extraction-mode=llm` doing nothing), and the shutdown sequencing (`:103-121`) is subtle enough to deserve a test.
4. **`internal/api/middleware.go`** — `withLogging` is never asserted; `withRecovery` is covered once via a nil-interface panic (`router_test.go:24`), which is good; CORS is asserted only for the presence of one header on a preflight (`:40-51`) — the `204` short-circuit and the method list are unchecked.
5. **`internal/db/seed.go`** — idempotency is claimed in the doc comment and never tested, though it is one `Seed`-twice-and-count away.

**Fix:** the two that pay for themselves immediately are `pageMedia` against a fake `PrivateRequest` (needs the interface seam from `integration_reviewer` I3/D2's fix — one interface serves both) and `Extract` against an `httptest` server (needs E7's `option.RequestOption` variadic). Both are small changes that convert the highest-risk code from untestable to tested. `run()` becomes testable by extracting the wiring into a `newServer(cfg) (*http.Server, func(), error)` that a test can call without listening.

### D6. No coverage measurement anywhere

**Rating: 4.0 / 10**

**Location:** `Makefile`, `.github/workflows/ci.yml`

No `-cover`, no `-coverprofile`, no report, no threshold — locally or in CI. I do not want a coverage *gate*, because a percentage target produces tests written to raise a number. I do want the number visible, because D5's ranking took manual reading to produce and a per-package coverage line would have surfaced the same conclusion in one command.

**Fix:** `go test ./... -coverprofile=cover.out && go tool cover -func=cover.out | tail -1` in the test target, and upload `cover.out` as a CI artifact. Per-package output makes "`internal/instagram` is at 20%" visible on every PR without anyone having to argue about a threshold.

### D7. CI builds an image it never runs, and never publishes one

**Rating: 5.6 / 10**

**Location:** `.github/workflows/ci.yml:81-90`

```yaml
docker:
  needs: [backend, frontend]
  steps:
    - name: Build image
      run: docker build -t recipe-reader:ci .
```

Building it catches Dockerfile drift, which the comment correctly claims and which is worth having. What it does not catch: whether the resulting image *starts*. Nothing runs the container, hits `/api/healthz`, or checks that the embedded frontend is the real build rather than the placeholder — which is precisely D1's failure mode, and it would survive this job untouched. The image is also discarded, so there is no artifact from a green build on `main`.

**Fix:** add a smoke step after the build — start the container against a Postgres service, poll `/api/healthz` until 200, and assert the served `/` is not the placeholder. That single step covers D1, the migration path, and the embed wiring at once, and it is the highest-value test in the whole pipeline per line of YAML. Then, on `main`, push to a registry with `docker/build-push-action`, tagged by commit SHA.

### D8. No version stamping

**Rating: 4.4 / 10**

**Location:** `Makefile:17-18`, `Dockerfile:20`

Both builds are plain `go build` with no `-ldflags`. The binary carries no version, commit, or build date, and there is no `--version` flag. Once an image is deployed, there is no way to determine what is running short of hashing the binary. For a project that ships as a single binary, that is the identity the whole packaging story rests on.

**Fix:** a `var version = "dev"` in `main`, stamped at build time, surfaced two ways — a `--version` flag (kong gives this nearly free via `kong.Vars` and a `Version` flag) and a field in the `/api/healthz` response, which makes a deployed instance self-identifying over HTTP:

```make
VERSION ?= $(shell git describe --tags --always --dirty)
build: frontend
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/recipe-reader ./cmd/recipe-reader
```

with a matching `ARG VERSION` in the Dockerfile.

## Low

### D9. `make check` skips the frontend entirely

**Rating: 3.4 / 10**

**Location:** `Makefile:32`

`check: lint vuln test` covers Go only. CI has a separate `frontend` job running `npm run typecheck` and `npm run build` (`ci.yml:64-79`), so a TypeScript error or a broken Vite build passes `make check` locally and fails after the push. The documented pre-commit gate does not represent the pipeline.

**Fix:** add a `frontend-check` target running typecheck and build, and include it in `check`. If the npm install cost is unwelcome on every run, gate it on `web/node_modules` existing.

### D10. `make lint` writes to a fixed `/tmp` path

**Rating: 2.2 / 10**

**Location:** `Makefile:20-21`

```make
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out
```

`/tmp/gofmt-out` is a fixed, world-writable path: two concurrent runs (two worktrees, two users on a shared box, two CI jobs on one runner) race on the same file, and the `;` separator means the `test` runs regardless of `tee`'s outcome. CI does this correctly at `ci.yml:42-44` with a shell variable and no temp file at all.

**Fix:** use CI's version and drop the file:

```make
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi
```

### D11. Unpinned tool versions make CI non-reproducible

**Rating: 3.0 / 10 — NARROWED.** The golangci-lint and govulncheck halves are withdrawn as policy-aligned. What holds is the sqlc asymmetry: `Makefile:11` regenerates with an unpinned local binary while `ci.yml:33` uses `sqlc@latest` and fails on any byte difference, so an upstream release reds every PR and CI's own remediation message can produce a third output.

**Location:** `.github/workflows/ci.yml:33` (`sqlc@latest`), `:52` (`golangci-lint version: latest`), `:55` (`govulncheck@latest`)

Three toolchains resolve to whatever is newest at run time, so a green commit can go red with no repository change — a golangci-lint release adding a check, or a sqlc release changing generated output, which would fail the codegen-drift gate at `:31-39` and block every PR until someone regenerates. The `vuln` target's comment explains the `@latest` choice deliberately (no global install, no drift between local and CI), and that reasoning is sound for govulncheck specifically, where a stale database is the greater risk.

This is a genuine tradeoff rather than a defect, and it points against the project's stated "always latest" policy, so I am recording it as a decision to make consciously rather than a bug to fix: **reproducibility and freshness cannot both be defaults here.** The usual resolution is to pin the tools that produce artifacts or gate merges (sqlc, golangci-lint) and keep `@latest` for the one whose value is freshness (govulncheck), with a scheduled job or Dependabot doing the bumping visibly.

### D12. The down migration is never executed

**Rating: 2.6 / 10** — merged with `persistence` P11; this reviewer owns the task, since it lands inside the D3 `TestMain` refactor. Needs a new exported Down entry point in `internal/db/connect.go`, not just a test.

**Location:** `internal/db/migrations/0001_init.down.sql`, `internal/db/testdb/testdb.go:46`

`testdb.New` only ever runs `db.Migrate` (up). The drop ordering in the down file is correct — children before parents — but nothing proves it, and it will rot the moment a `0002` lands. A rollback you have never run is not a rollback plan.

**Fix:** one test that migrates up, down, and up again against a throwaway container. With D3's `TestMain` in place it is a few lines in `internal/db`.

### D13. No `HEALTHCHECK` in the image

**Rating: 2.8 / 10**

**Location:** `Dockerfile`, `docker-compose.yml:18-36`

The `db` service has a proper healthcheck with `depends_on: condition: service_healthy` (`:12-16,20-22`) — the app waits for Postgres correctly. The `app` service has no healthcheck of its own, and the image defines none, so an orchestrator has no way to know the process is serving. `GET /api/healthz` exists and is exactly the endpoint for it (`internal/api/router.go:34`).

**Fix:** a `HEALTHCHECK` in the Dockerfile hitting `/api/healthz`. The runtime stage is `alpine` without `curl` or `wget`, so either add `wget` via the existing `apk add` line or give the binary a `--health-check` subcommand that dials itself — the latter keeps the image minimal and works in `scratch` if the base ever shrinks.

## What's right

Both of the things my brief calls release blockers are present and well built, and several details show real release-engineering care:

- **The CI workflow is genuinely good.** `permissions: contents: read` at the top; `concurrency` with `cancel-in-progress` and a comment explaining that cancelled runs save real container minutes; per-job `timeout-minutes` on all three jobs; `needs: [backend, frontend]` so the expensive image build only runs after the cheap gates pass; `setup-node` with npm caching keyed on `web/package-lock.json`.
- **The sqlc drift gate (`ci.yml:28-39`) is the standout.** Generated-and-committed code silently diverging from its source is the classic sqlc failure, and regenerating plus diffing with `git status --porcelain` — complete with an `::error::` annotation telling the developer exactly which make target to run — is the correct solution. Most projects discover this drift months later.
- **The Docker build is properly staged and reasoned.** Three stages, `npm ci` rather than `install` with the lockfile argument written down, `go mod download` before `COPY . .` for layer caching, `CGO_ENABLED=0` for a static binary on Alpine, a non-root uid, and `ca-certificates` installed with an explicit note that the outbound HTTPS calls fail without it. The `.dockerignore` is thought through rather than copied — it excludes `internal/webui/dist` *specifically* so the committed placeholder cannot shadow the real build, which is the exact hazard D1 describes on the Make path.
- **`testdb` is well built even though it is over-invoked.** `BasicWaitStrategies()` is passed explicitly, with a comment explaining that v0.44's `tcpostgres.Run` sets no wait strategy and that migration races startup without it — a specific race most projects meet in CI instead. Cleanup is registered via `t.Cleanup` for both container and pool, so no test leaks either.
- **Tests run against real Postgres, not a stand-in.** Five packages exercise actual schema, constraints, and transaction semantics. The container cost (D3) is a real tradeoff and the right side of it was chosen — the fix is to pay it fewer times, not to give it up.
- **The test suite is honest where it exists.** `handlers_import_test.go` covers the nil-worker 503 path; `router_test.go:24` deliberately triggers a nil-interface panic to prove the recovery middleware returns 500 rather than dropping the connection; `embed_test.go:10-11` explicitly asserts behaviour rather than page content so it holds for both the placeholder and a real build; `worker_test.go:46` proves the single-flight guard with a call counter rather than a sleep-and-hope. `config_test.go` is the largest non-database test file in the repo, which is the right instinct for the layer that turns operator input into runtime behaviour.
- **`make check` exists and aggregates the right three gates** (lint, vuln, test), and `make vuln` uses `go run ...@latest` with a comment explaining that it keeps a fresh clone working and prevents local/CI drift. The intent behind the pre-commit gate is sound; D4 and D9 are about closing the remaining distance to what CI actually runs.
