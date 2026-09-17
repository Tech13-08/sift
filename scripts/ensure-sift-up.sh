#!/usr/bin/env bash
# Bring the k3d prod cluster back after Docker/WSL reboot.
# Idempotent: safe to run manually or from the Windows boot task.
set -euo pipefail

CLUSTER="${SIFT_K3D_CLUSTER:-sift-cluster}"
CONTEXT="k3d-${CLUSTER}"
LOG="${SIFT_ENSURE_LOG:-$HOME/.local/state/sift/ensure-up.log}"
TIMEOUT_DOCKER="${SIFT_DOCKER_WAIT_SECS:-180}"
TIMEOUT_PODS="${SIFT_PODS_WAIT_SECS:-300}"

mkdir -p "$(dirname "$LOG")"
exec >>"$LOG" 2>&1
echo "===== $(date -Is) ensure-sift-up start ====="

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1"; exit 1; }; }
need docker
need k3d
need kubectl

echo "waiting for Docker Engine (up to ${TIMEOUT_DOCKER}s)…"
deadline=$((SECONDS + TIMEOUT_DOCKER))
until docker info >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    echo "ERROR: Docker Engine not ready"
    exit 1
  fi
  sleep 3
done
echo "Docker OK"

if ! k3d cluster list 2>/dev/null | awk 'NR>1 {print $1}' | grep -qx "$CLUSTER"; then
  echo "ERROR: k3d cluster '$CLUSTER' does not exist - run: make start-world"
  exit 1
fi

echo "starting k3d cluster $CLUSTER (no-op if already up)…"
k3d cluster start "$CLUSTER" || true

kubectl config use-context "$CONTEXT" >/dev/null

echo "waiting for core pods (up to ${TIMEOUT_PODS}s)…"
apps=(postgres redis auth-service web-service ingestion-service summarizer-service discord-service cloudflared)
deadline=$((SECONDS + TIMEOUT_PODS))
for app in "${apps[@]}"; do
  while true; do
    ready="$(kubectl get deploy "$app" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)"
    ready="${ready:-0}"
    if [[ "$ready" =~ ^[1-9] ]]; then
      echo "  $app ready"
      break
    fi
    if (( SECONDS >= deadline )); then
      echo "ERROR: timed out waiting for deploy/$app"
      kubectl get pods || true
      exit 1
    fi
    sleep 5
  done
done

echo "pods:"
kubectl get pods -o wide || true

# Weekly-ish DB dump when ensure-up runs (skips if a backup < 7d old). Tiny gzip; not a CronJob.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -x "$ROOT/scripts/backup-db.sh" ]] || chmod +x "$ROOT/scripts/backup-db.sh" 2>/dev/null; then
  "$ROOT/scripts/backup-db.sh" || echo "WARN: backup-db failed (non-fatal)"
fi

echo "===== $(date -Is) ensure-sift-up OK ====="
