#!/usr/bin/env bash
# Lightweight Postgres dump for k3d prod. Gzipped; keeps a small rolling set.
# Safe to run from ensure-sift-up (skips if a fresh backup already exists).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="${SIFT_BACKUP_DIR:-$ROOT/backups}"
KEEP="${SIFT_BACKUP_KEEP:-5}"
MAX_AGE_SECS="${SIFT_BACKUP_MAX_AGE_SECS:-604800}" # 7 days
FORCE=0
if [[ "${1:-}" == "--force" ]]; then
  FORCE=1
fi

mkdir -p "$DIR"

newest="$(ls -1t "$DIR"/sift-*.sql.gz 2>/dev/null | head -1 || true)"
if [[ "$FORCE" -eq 0 && -n "$newest" ]]; then
  # Prefer GNU/BSD stat; fall back to always backup if age unknown.
  age=999999999
  if age_sec="$(stat -c %Y "$newest" 2>/dev/null)"; then
    age=$(( $(date +%s) - age_sec ))
  elif age_sec="$(stat -f %m "$newest" 2>/dev/null)"; then
    age=$(( $(date +%s) - age_sec ))
  fi
  if (( age < MAX_AGE_SECS )); then
    echo "backup skip: $(basename "$newest") is ${age}s old (< ${MAX_AGE_SECS}s)"
    exit 0
  fi
fi

stamp="$(date +%Y%m%d-%H%M%S)"
out="$DIR/sift-${stamp}.sql.gz"
tmp="${out}.partial"
rm -f "$DIR"/sift-*.sql.gz.partial 2>/dev/null || true

echo "backup: dumping to $out"
kubectl exec deploy/postgres -- pg_dump -U postgres --no-owner --no-acl sift | gzip -1 >"$tmp"
mv "$tmp" "$out"
echo "backup: wrote $(du -h "$out" | awk '{print $1}')"

# Keep newest KEEP dumps only.
mapfile -t old < <(ls -1t "$DIR"/sift-*.sql.gz 2>/dev/null | tail -n +"$((KEEP + 1))" || true)
for f in "${old[@]:-}"; do
  [[ -n "$f" ]] || continue
  rm -f "$f"
  echo "backup: pruned $(basename "$f")"
done
