#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/deploy/docker-compose.test.yml"
PULL_POLICY="${COMPOSE_PULL_POLICY:-missing}"
DATABASE_URL="${DATABASE_URL:-postgres://gophprofile:gophprofile@localhost:15432/gophprofile_test?sslmode=disable}"

cleanup() {
  docker compose -f "$COMPOSE_FILE" down --volumes --remove-orphans >/dev/null 2>&1 || true
}

trap cleanup EXIT

docker compose -f "$COMPOSE_FILE" up --pull "$PULL_POLICY" -d --wait postgres-test
docker compose -f "$COMPOSE_FILE" run --rm migrate-test

DATABASE_URL="$DATABASE_URL" go test -tags=integration ./internal/storage/postgres -run TestIntegration -count=1 -v
