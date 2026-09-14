# Sift

Prototype: Discord login, Gmail inbox watch, Postgres `ingested_messages`, daily Ollama digest, Discord DM.

This repo is meant to be cloned and run locally with Docker Compose. **Do not commit secrets.** Each developer (and each machine) uses a private `.env`.

## What you need

- Docker and Docker Compose
- A [Discord application](https://discord.com/developers/applications) (OAuth2, `identify`)
- A Google Cloud project with:
  - Gmail API enabled
  - OAuth client (Web application)
  - A Pub/Sub topic for Gmail `users.watch` (only required to ingest new mail)

## Quick start

```bash
git clone <this-repo-url>
cd sift
./scripts/bootstrap.sh
```

Edit `.env` and fill in Discord + Google values. Then:

```bash
docker compose up --build
```

Open http://localhost:3000 — Login with Discord, then Link Gmail.

| Service | URL |
|---|---|
| Auth | http://localhost:3000 |
| Ingestion webhook | http://localhost:${INGESTION_HOST_PORT:-8080}/webhooks/gmail |
| Summarizer health | http://localhost:8090/healthz |

Compose reads `.env` from the repo root. Schema is applied from `infra/postgres/init.sql` on first Postgres start.

## Create OAuth apps

### Discord

1. New application → OAuth2
2. Redirect URL: `http://localhost:3000/auth/discord/callback`
3. Copy Client ID and Client Secret into `.env`

### Google

1. APIs & Services → Credentials → Create OAuth client (Web)
2. Redirect URL: `http://localhost:3000/auth/google/callback`
3. Enable Gmail API
4. Copy Client ID and Client Secret into `.env`
5. Create a Pub/Sub topic and put the full name in `GOOGLE_PUBSUB_TOPIC`:
   `projects/YOUR_PROJECT/topics/YOUR_TOPIC`
6. Grant `gmail-api-push@system.gserviceaccount.com` permission to publish to that topic
7. Point a Pub/Sub **push** subscription at your Hookdeck source URL (`https://hkdk.events/...`).
8. Stop the subscription from vanishing after ~31 idle days:
   `gcloud pubsub subscriptions update YOUR_SUBSCRIPTION --expiration-period=never`
   `expirationPolicy: {}` on `describe` means it never expires.

Compose runs `hookdeck listen` as `sift-hookdeck` so you do not need a dedicated terminal. It forwards on the Docker network to `http://ingestion-service:8080/webhooks/gmail` (not the host port). Auth comes from `~/.config/hookdeck` mounted into the container, or from `HOOKDECK_API_KEY` in `.env`. Do not run a second `hookdeck listen` on the host at the same time.

Guest Hookdeck URLs can die. Claim the same sandbox (do **not** `hookdeck logout` first):

```bash
hookdeck login
hookdeck whoami   # should not say guest
docker compose restart hookdeck
```

If `whoami` still says guest, the Pub/Sub endpoint is not durable yet. After a real login, confirm the push endpoint is still the same `hkdk.events` URL (`gcloud pubsub subscriptions describe YOUR_SUBSCRIPTION`).

Gmail watches expire in about 7 days. Auth sets a timer for 24 hours before each mailbox's stored expiry (and again on process start). Google is only called when that timer fires. While `NODE_ENV` is not `production`, a logged-in session can also hit `/test-refresh`.

The summarizer fires one digest per user at `digest_local_time` in that user's `timezone` (default 08:00 in that zone). It keeps mail that would cost something to miss (a time, a person waiting, money, an outcome), skips marketing, applies per-user Discord rules, then DMs a compiled note plus a hygiene line when a promo list has emailed often. Digests paginate across Discord messages when there are more than 10 embeds (nothing is dropped). When a day has mail from more than one linked inbox, kept items are clustered by mailbox. Compose reaches Ollama at `host.docker.internal:11434` (`OLLAMA_URL` / `OLLAMA_MODEL`) for leftover mail. `POST http://localhost:8090/test-digest` and `POST http://localhost:8090/test-recent` run when `DIGEST_TEST_ENDPOINT=1`. Test endpoints DM a preview and do not consume the next scheduled digest.

Every keep/skip is logged as `decide keep|skip stage=... why=...` on the summarizer (`docker logs sift-summarizer`). Qwen also stores a short why on each row's `outcome` column.

DM the bot after linking Discord (plain language). It stores standing instructions and appends them to the next digest. New keep rules use a grey embed until you pick a color (`make it purple`). Skip/mute rules have no color. You can combine edits (`remove 3, 4, 5 and change rule 2 to blue`). Ask about stored mail (`did I get any Hyundai emails today?`, `important emails from the past 24 hours`, or `anything about the car?`). Brand matches win; “what was important” uses kept mail, not a keyword search. Fuzzy questions can use a local embedding over the same Postgres (pgvector + Ollama `nomic-embed-text`). The digest does not use RAG. Say `help` for commands. Mail bodies are dropped after 30 days; the rest of each row after 90.

## Webhook secret

If `WEBHOOK_SECRET` is set, ingestion requires header `X-Webhook-Secret`. Compose upserts that header onto the CLI destination (`cli-sift-webhook`) on Hookdeck start. Recreate `sift-ingestion` and `sift-hookdeck` after changing `.env`.

If it is unset, the webhook is unauthenticated and the service logs a warning. Do not expose the ingestion host port publicly without a secret.

Host ports must be unique on this machine. If another Compose project already binds 8080 (for example stardew-server's API), set `INGESTION_HOST_PORT` in `.env` to a free port. That only affects your browser/curl on the laptop. Hookdeck inside Compose still uses `ingestion-service:8080`.

## Kubernetes (optional)

Local path is `k8s/manifests/` and a k3d cluster named `sift-cluster`. The Makefile is the source of truth; ignore older `deploy/k8s` docs.

```bash
cp k8s/manifests/config/secrets.local.yaml.example k8s/manifests/config/secrets.local.yaml
# edit secrets.local.yaml — never commit it
make start-world
```

`make apply-config` uses `secrets.local.yaml` when present, otherwise the placeholder `secrets.yaml`.

OAuth redirect URIs in the ConfigMap are `http://localhost:3000/...`. Change them if the cluster is not reached via localhost.

`make nuke` deletes **only** resources from this repo's manifests, not the rest of the cluster.

## Repo layout

```
services/auth-service/       Discord + Google OAuth, Gmail watch
services/ingestion-service/  Pub/Sub webhook → Gmail history → ingested_messages
services/summarizer-service/ Daily digest clock → Ollama → Discord DM
infra/postgres/init.sql      Schema (users, oauth_credentials, digests, ingested_messages)
infra/postgres/schema.md     ERD (mermaid; keep in sync with init.sql)
k8s/manifests/               Cluster config, infra, apps
scripts/bootstrap.sh         Creates .env and k8s secrets.local.yaml from examples
```

## Secrets policy

Never commit:

- `.env`
- `k8s/manifests/config/secrets.local.yaml`
- OAuth client secrets, bot tokens, session secrets, database passwords

Tracked stand-ins:

- `.env.example`
- `k8s/manifests/config/secrets.yaml` (placeholders)
- `k8s/manifests/config/secrets.local.yaml.example`

If this tree ever held real credentials (local files, old ConfigMaps, chat logs), **rotate** Discord client secret, Discord bot token, Google client secret, and `SESSION_SECRET` in the provider consoles, then update your private `.env`.

OAuth access/refresh tokens are encrypted at rest with `TOKEN_ENCRYPTION_KEY` (AES-256-GCM). Losing that key means every Gmail inbox must be relinked.

## Current gaps (not required to clone)

- Digest is compiled from stored facts and per-user Discord rules; leftover mail still uses Ollama
- Google OAuth clients in Testing expire refresh tokens after 7 days until the app is published
