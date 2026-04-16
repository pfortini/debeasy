#!/usr/bin/env bash
# Dev helper: spins up Postgres/MySQL and runs debeasy on :8080.
set -euo pipefail
cd "$(dirname "$0")/.."

docker compose -f docker-compose.dev.yml up -d
echo "waiting for postgres + mysql to be healthy..."
for i in $(seq 1 30); do
  pg=$(docker inspect -f '{{.State.Health.Status}}' debeasy-pg 2>/dev/null || echo none)
  my=$(docker inspect -f '{{.State.Health.Status}}' debeasy-mysql 2>/dev/null || echo none)
  if [[ "$pg" == "healthy" && "$my" == "healthy" ]]; then
    break
  fi
  sleep 2
done
echo "pg=$pg  mysql=$my"

DEBEASY_DATA_DIR=${DEBEASY_DATA_DIR:-./.debeasy}
mkdir -p "$DEBEASY_DATA_DIR"
exec go run ./cmd/debeasy --addr :8080 --data-dir "$DEBEASY_DATA_DIR"
