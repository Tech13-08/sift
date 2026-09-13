#!/bin/sh
set -e
SOURCE="${HOOKDECK_SOURCE:-sift-webhook}"
DEST="cli-${SOURCE}"

if [ -n "$WEBHOOK_SECRET" ]; then
  hookdeck gateway destination upsert "$DEST" \
    --type CLI \
    --cli-path /webhooks/gmail \
    --auth-method api_key \
    --api-key "$WEBHOOK_SECRET" \
    --api-key-header X-Webhook-Secret \
    --api-key-to header \
    --output json >/dev/null
fi

exec hookdeck listen http://ingestion-service:8080 "$SOURCE" \
  --path /webhooks/gmail \
  --output compact \
  --no-healthcheck \
  --device-name sift-compose
