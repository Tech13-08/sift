#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

if [[ ! -f .env ]]; then
  cp .env.example .env
  echo "Created .env from .env.example — fill in Discord and Google credentials."
else
  echo ".env already exists; leaving it unchanged."
fi

if grep -Eq '^TOKEN_ENCRYPTION_KEY=$|^TOKEN_ENCRYPTION_KEY=replace-with-64-hex-chars$' .env || ! grep -q '^TOKEN_ENCRYPTION_KEY=' .env; then
  key="$(openssl rand -hex 32)"
  if grep -q '^TOKEN_ENCRYPTION_KEY=' .env; then
    tmp="$(mktemp)"
    sed "s/^TOKEN_ENCRYPTION_KEY=.*/TOKEN_ENCRYPTION_KEY=${key}/" .env > "$tmp"
    mv "$tmp" .env
  else
    printf '\nTOKEN_ENCRYPTION_KEY=%s\n' "$key" >> .env
  fi
  echo "Set TOKEN_ENCRYPTION_KEY in .env (not printed)."
fi

local_secrets="k8s/manifests/config/secrets.local.yaml"
example_secrets="k8s/manifests/config/secrets.local.yaml.example"
if [[ ! -f "$local_secrets" ]]; then
  cp "$example_secrets" "$local_secrets"
  echo "Created $local_secrets — fill in the same credentials for k8s."
else
  echo "$local_secrets already exists; leaving it unchanged."
fi

echo
echo "Next:"
echo "  1. Edit .env (and secrets.local.yaml if you use k8s)."
echo "  2. docker compose up --build"
echo "  3. Open http://localhost:3000"
