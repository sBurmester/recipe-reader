#!/usr/bin/env bash
# Smoke-tests a built recipe-reader image: starts it against a throwaway
# Postgres, waits for /api/healthz, checks that a database-backed route answers
# (so the migrations ran), and checks that / serves the real frontend rather
# than the placeholder page a build without the frontend embeds.
#
# CI runs this after `docker build`; it runs the same way locally.
#
# Usage: scripts/smoke-test-image.sh [image]    (default: recipe-reader:ci)
set -euo pipefail

image=${1:-recipe-reader:ci}
repo=$(cd "$(dirname "$0")/.." && pwd)
placeholder="$repo/internal/webui/placeholder/index.html"

suffix=$$
net=rr-smoke-net-$suffix
db=rr-smoke-db-$suffix
app=rr-smoke-app-$suffix

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		echo "--- app container logs" >&2
		docker logs "$app" 2>&1 | tail -n 50 >&2 || true
	fi
	docker rm -f "$app" "$db" >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
	exit "$status"
}
trap cleanup EXIT

fail() {
	echo "smoke test FAILED: $*" >&2
	exit 1
}

docker network create "$net" >/dev/null
docker run -d --name "$db" --network "$net" \
	-e POSTGRES_DB=recipes -e POSTGRES_USER=recipes -e POSTGRES_PASSWORD=recipes \
	--health-cmd "pg_isready -U recipes" --health-interval 1s --health-retries 60 \
	postgres:17-alpine >/dev/null

for _ in $(seq 1 60); do
	[ "$(docker inspect -f '{{.State.Health.Status}}' "$db")" = healthy ] && break
	sleep 1
done
[ "$(docker inspect -f '{{.State.Health.Status}}' "$db")" = healthy ] || fail "postgres did not become healthy"

# HTTP_ADDR must bind every interface for the published port to reach it, and
# the server refuses that without a token — the same constraint compose has.
docker run -d --name "$app" --network "$net" -p 127.0.0.1::8080 \
	-e DB_DSN="postgres://recipes:recipes@$db:5432/recipes?sslmode=disable" \
	-e HTTP_ADDR=":8080" \
	-e API_TOKEN=smoke-test-token \
	"$image" >/dev/null

port=$(docker port "$app" 8080/tcp | head -n 1 | sed 's/.*://')
base="http://127.0.0.1:$port"

healthy=false
for _ in $(seq 1 60); do
	if curl -fsS "$base/api/healthz" 2>/dev/null | grep -q '"status":"ok"'; then
		healthy=true
		break
	fi
	[ "$(docker inspect -f '{{.State.Running}}' "$app")" = true ] || fail "the app container exited during startup"
	sleep 1
done
$healthy || fail "/api/healthz did not answer ok within 60s"

# Liveness touches no dependency by design, so reach the database separately:
# this only answers once the migrations have run against real Postgres.
curl -fsS "$base/api/recipes" | grep -q '"recipes"' || fail "GET /api/recipes did not answer"

# Compared against the placeholder file itself rather than a marker string, so
# rewording the placeholder cannot quietly disable the check.
body=$(curl -fsS "$base/")
[ "$body" != "$(cat "$placeholder")" ] || fail "the image serves the placeholder frontend, not a real build"
grep -qi '<script' <<<"$body" || fail "GET / does not look like the built application"

echo "smoke test passed: $image"
