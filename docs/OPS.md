# Operating Sift

**Production is k3d**: running the apps, a Postgres PVC, and an in-cluster Cloudflare tunnel.

## After Server reboot

On this server, Sift is set up to heal after reboot once Windows reaches user session.

1. **Docker Desktop** starts via its own boot task + `AutoStart=true`
2. k3d containers use `restart=unless-stopped`
3. Task `Sift Ensure Up` (AtLogOn + 2m) + Startup `SiftEnsureUp.cmd` start **Ollama**, wait for Docker, then run `scripts/ensure-sift-up.sh` (`k3d cluster start` + wait for pods, including cloudflared)


| Piece           | How it starts                                                                 |
| --------------- | ----------------------------------------------------------------------------- |
| Docker Desktop  | Boot task + Docker AutoStart                                                  |
| Ollama          | Boot task **Sift Start Ollama** (no sign-in; `make install-ollama-autostart`) |
| k3d + Sift pods | `scripts/ensure-sift-up.sh` via **Sift Ensure Up** (AtLogOn)                  |


Logs:

- Windows: `%LOCALAPPDATA%\Sift\ensure-up.log`
- WSL: `~/.local/state/sift/ensure-up.log`

Manual heal / reinstall:

```bash
# From WSL
make ensure-up

# Re-register Windows task + Startup cmd
make install-autostart

# Kick the task now
powershell.exe -NoProfile -Command "Start-ScheduledTask -TaskName 'Sift Ensure Up'"
```

## Daily health (k3d)

```bash
cd /path/to/sift
kubectl config use-context k3d-sift-cluster
kubectl get pods
```

Expect Running: `web-service`, `auth-service`, `ingestion-service`, `summarizer-service`, `discord-service`, `postgres`, `redis`, `cloudflared`.

```bash
kubectl exec deploy/web-service -- wget -qO- http://auth-service:3000/healthz
kubectl exec deploy/summarizer-service -- wget -qO- http://127.0.0.1:8090/healthz
curl -sfI https://sift.falaktulsi.com/
curl -sfI http://localhost:8088/    # host map to Traefik
```

Logs:

```bash
kubectl logs deploy/auth-service --tail=100
kubectl logs deploy/ingestion-service --tail=100
kubectl logs deploy/summarizer-service --tail=200
kubectl logs deploy/discord-service --tail=100
kubectl logs deploy/cloudflared --tail=50
```

Redeploy after code change: `make sync-web` / `make sync-all`.

## External services

### Cloudflare tunnel (k3d)

cloudflared is **in-cluster**. Public hostname service:

`http://traefik.kube-system.svc.cluster.local:80`

```bash
kubectl logs deploy/cloudflared --tail=50
curl -sfI https://sift.falaktulsi.com/webhooks/gmail   
```

### Google Cloud / Pub/Sub (prod mail path)

Gmail `users.watch` → topic `SiftIncomingEmail` → push subscription `sift-gmail-push` →

`https://sift.falaktulsi.com/webhooks/gmail?token=<WEBHOOK_SECRET>`

Pub/Sub cannot set custom headers, so the secret is the `token` query param (ingestion also still accepts `X-Webhook-Secret`).

```bash
make pubsub-point-at-public
gcloud pubsub subscriptions describe projects/sift-emailreader/subscriptions/sift-gmail-push
```

OAuth clients: Cloud Console → Credentials. Gmail API enabled. Testing-mode refresh tokens expire ~7 days until the app is published.

### Hookdeck (legacy - local testing without a domain)

Before the public tunnel/domain, mail was relayed with Hookdeck (`hkdk.events` → Compose `ingestion-service`). That path is **not used in prod** anymore.

```bash
docker compose up -d db redis ingestion-service hookdeck
hookdeck whoami   # should be a real account
# Point Pub/Sub push back at the Hookdeck source URL temporarily, restorable with:
make pubsub-point-at-public
```

### Discord

- Bot online, intents, OAuth redirects on `https://sift.falaktulsi.com/...`
- Support / bot invite env vars from `/api/me`
- Shared guild required for reliable DMs

### Ollama (host)

Pods use `host.docker.internal:11434`. Confirm models: `OLLAMA_MODEL`, `OLLAMA_EMBED_MODEL`.

Ollama must be running on the Windows host. Boot without sign-in: `make install-ollama-autostart` (one UAC prompt) registers task **Sift Start Ollama**.

## Monitoring (Grafana + Prometheus)

Public read-only Grafana for recruiters / portfolio: **https://grafana-sift.falaktulsi.com** (anonymous Viewer). Prometheus UI stays in-cluster only.

```bash
make install-monitoring
```

Installs `kube-prometheus-stack` in namespace `monitoring`, plus Redis/Postgres exporters and the **Sift cluster health** dashboard.

Cloudflare (same tunnel as the app) - one-time public hostname:

| Field | Value |
| ----- | ----- |
| Hostname | `grafana-sift.falaktulsi.com` |
| Type | HTTP |
| URL | `http://traefik.kube-system.svc.cluster.local:80` |

DNS: CNAME `grafana-sift` → same target as `sift` on `falaktulsi.com`.

Local check before DNS: `curl -sfI -H 'Host: grafana-sift.falaktulsi.com' http://localhost:8088/`

Admin password (not for the public link): set `GRAFANA_ADMIN_PASSWORD` in `.env`, then `make install-monitoring`. Login is `admin` / that value. Home dashboard is a glanceable status board (green/red tiles) plus **live product activity** counts from auth-service (no usernames/emails). Bundled Kubernetes dashboards are disabled.

## Data & retention

- **Prod DB:** k3d PVC `postgres-pvc`
- **Compose backup volume:** `sift_postgres_data` (cold; do not run two Postgres writers)
- Redis: no PVC - sessions reset if Redis pod recreated
- Bodies null after 30d; rows delete after 90d
- Digest window capped at `DIGEST_MAX_MESSAGES` (default 400)

```bash
kubectl exec -i deploy/postgres -- pg_dump -U postgres sift > sift-$(date +%F).sql
```

Re-copy Compose → k3d: `make migrate-db-from-compose`.

## Auth notes

- Register on `/`; legacy usernames claim password once via Create account
- Unlink Gmail: **Inboxes**. 
- Discord link/unlink: **Home**. 
- Change password / delete account: **Account** (username in header)
- Never set `DIGEST_TEST_ENDPOINT=1` on the public host

### SMTP (password-reset email)

Without SMTP, forgot-password links are **logged** by auth-service. Prod uses **Resend** over SMTP. Add to `.env` then `make sync-secrets-from-env` and restart auth:

Created an API key in the [Resend dashboard](https://resend.com/api-keys). Domain `sift.falaktulsi.com` verified there (DNS). Then:

```bash
# in .env
SMTP_HOST=smtp.resend.com
SMTP_PORT=465
SMTP_SECURE=true
SMTP_USER=resend
SMTP_PASS=re_xxxxxxxx
SMTP_FROM=Sift <noreply@sift.falaktulsi.com>
```

Then:

```bash
make sync-secrets-from-env
kubectl rollout restart deploy/auth-service
```

