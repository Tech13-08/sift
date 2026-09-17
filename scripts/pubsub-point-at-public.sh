#!/usr/bin/env bash
# Point Gmail Pub/Sub push at the public domain webhook (no Hookdeck).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

SUB="${PUBSUB_SUBSCRIPTION:-projects/sift-emailreader/subscriptions/sift-gmail-push}"
HOST="${SIFT_PUBLIC_HOST:-https://sift.falaktulsi.com}"

if [[ ! -f .env ]]; then
  echo "missing .env" >&2
  exit 1
fi
SECRET="$(grep -E '^WEBHOOK_SECRET=' .env | head -1 | cut -d= -f2- | tr -d '"' | tr -d "'")"
if [[ -z "$SECRET" ]]; then
  echo "WEBHOOK_SECRET empty in .env - refusing to publish an open webhook URL" >&2
  exit 1
fi

ENDPOINT="${HOST}/webhooks/gmail?token=${SECRET}"

echo "Updating ${SUB}"
echo "  push endpoint host: ${HOST}/webhooks/gmail?token=***"
gcloud pubsub subscriptions update "$SUB" \
  --push-endpoint="$ENDPOINT" \
  --expiration-period=never

gcloud pubsub subscriptions describe "$SUB" \
  --format='yaml(name,pushConfig.pushEndpoint,expirationPolicy,state)' \
  | sed -E 's/(token=)[^&\"]*/\1***/'
