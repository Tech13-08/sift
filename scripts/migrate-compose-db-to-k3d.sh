#!/usr/bin/env bash
# One-way migrate: Compose Postgres (sift-db) → k3d Postgres PVC.
# Requires: docker compose db up, kubectl context k3d-sift-cluster.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if [[ ! -f .env ]]; then
  echo "missing .env" >&2
  exit 1
fi
set -a
DB_USER="$(grep -E '^DB_USER=' .env | head -1 | cut -d= -f2- | tr -d '"' | tr -d "'")"
DB_NAME="$(grep -E '^DB_NAME=' .env | head -1 | cut -d= -f2- | tr -d '"' | tr -d "'")"
set +a
DB_USER="${DB_USER:-postgres}"
DB_NAME="${DB_NAME:-sift}"

dump="$(mktemp /tmp/sift-migrate.XXXXXX.sql)"
cleanup() { rm -f "$dump"; }
trap cleanup EXIT

echo "Dumping Compose database from container sift-db (user=${DB_USER} db=${DB_NAME})..."
docker exec sift-db pg_dump -U "$DB_USER" --clean --if-exists --no-owner --no-acl "$DB_NAME" > "$dump"
echo "Dump size: $(wc -c < "$dump") bytes"

echo "Restoring into k3d deploy/postgres..."

kubectl scale deploy/auth-service deploy/ingestion-service deploy/summarizer-service deploy/discord-service deploy/web-service --replicas=0 >/dev/null
kubectl wait --for=delete pod -l 'app in (auth-service,ingestion-service,summarizer-service,discord-service,web-service)' --timeout=120s 2>/dev/null || true

kubectl exec -i deploy/postgres -- psql -U postgres -d sift -v ON_ERROR_STOP=1 < "$dump" >/dev/null

kubectl scale deploy/auth-service deploy/ingestion-service deploy/summarizer-service deploy/discord-service deploy/web-service --replicas=1 >/dev/null
kubectl rollout status deploy/auth-service deploy/web-service --timeout=180s >/dev/null

echo "Migrate complete. k3d Postgres PVC now has Compose data."
echo "Next: stop Compose app/db/tunnel so only k3d is prod (make stop-compose-prod)."
