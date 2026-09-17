# Kubernetes / k3d (production)

**k3d is the prod runtime.**

Local path: `k8s/manifests/` + k3d cluster `sift-cluster`.

## Promote to prod (from Compose)

```bash
# 1. Cluster already up? Otherwise: make start-world
make promote-k3d-prod
```

This will:

1. Sync `.env` → `secrets.local.yaml` (including `TOKEN_ENCRYPTION_KEY` + `CF_TUNNEL_TOKEN`)
2. Apply public ConfigMap (`https://sift.falaktulsi.com`) + Ingress + apps (incl. cloudflared)
3. **Migrate** Compose Postgres (`sift_postgres_data`) → k3d `postgres-pvc`
4. Stop Compose tunnel/web/auth/ingestion/summarizer/discord/hookdeck/db/redis

**You must update Cloudflare** (one-time if not already):

Zero Trust → Tunnels → Public Hostname for `sift.falaktulsi.com`:


| Field | Value                                      |
| ----- | ------------------------------------------ |
| Type  | HTTP                                       |
| URL   | `traefik.kube-system.svc.cluster.local:80` |


Then wire Gmail (drops Hookdeck):

```bash
make pubsub-point-at-public
```

That sets Pub/Sub push to `https://sift.falaktulsi.com/webhooks/gmail?token=…` with never-expire.

## Day-to-day

```bash
make create-cluster
kubectl get pods
kubectl logs deploy/cloudflared --tail=50
kubectl logs deploy/web-service --tail=50

make sync-web / sync-all
make sync-secrets-from-env    # after .env changes
make migrate-db-from-compose  # re-copy Compose DB → k3d (scales apps down briefly)
make stop-compose-prod
```

UI: **[https://sift.falaktulsi.com](https://sift.falaktulsi.com)** (tunnel) or [http://localhost:8088](http://localhost:8088) (host ingress map).

OAuth redirects (Google/Discord) must stay on `https://sift.falaktulsi.com/auth/.../callback`.

## Data


| Store                 | Location                                                            |
| --------------------- | ------------------------------------------------------------------- |
| Prod Postgres         | k3d PVC `postgres-pvc`                                              |
| Compose backup volume | Docker volume `sift_postgres_data` (kept after `stop-compose-prod`) |


Volumes are **not** bind-mounted both ways (two Postgres writers would corrupt data). Cutover is dump/restore; Compose volume remains a cold backup.

## Notes

- Ollama: `host.docker.internal:11434` on the Windows/Docker host (`make install-ollama-autostart` for boot without sign-in)
- Schema source of truth: `infra/postgres/init.sql` (ConfigMap on first PVC init)
- `DIGEST_TEST_ENDPOINT` unset in cluster
- Monitoring: `make install-monitoring` → Grafana at `grafana-sift.falaktulsi.com` (see [docs/OPS.md](../docs/OPS.md#monitoring-grafana--prometheus))
- `make nuke` removes Sift workloads; `make delete-cluster` destroys k3d

