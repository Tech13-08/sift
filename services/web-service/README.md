# Sift web UI

Next.js App Router UI for onboarding, inboxes, schedule, rules, digests, and ask.

Proxies `/auth/*` and `/api/*` to auth-service. Accounts are username/password; Gmail and Discord are linked from the signed-in app.

## Local

```bash
AUTH_INTERNAL_URL=http://localhost:3000 npm run dev
```

## Compose

Published at `http://localhost:${WEB_HOST_PORT:-3010}`.

OAuth redirects (Gmail / Discord link):
- `http://localhost:3010/auth/google/callback`
- `http://localhost:3010/auth/discord/callback`
