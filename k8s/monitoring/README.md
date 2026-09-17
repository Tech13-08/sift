# Monitoring (k3d)

`make install-monitoring` installs Prometheus + Grafana (`kube-prometheus-stack`) into namespace `monitoring`.

- Public: https://grafana-sift.falaktulsi.com (anonymous Viewer)
- Admin: `admin` / `GRAFANA_ADMIN_PASSWORD` from `.env` (see `.env.example`)
- Home: **Sift cluster health** - green/red service tiles + a couple of quiet sparklines
- Live activity: auth-service counters (`sift_user_actions_total`) - signups, Gmail/Discord link/unlink, etc. (no PII)
- Bundled kube-prometheus dashboards are off (`defaultDashboardsEnabled: false`)
- Values: [values.yaml](./values.yaml)
- Exporters: [exporters.yaml](./exporters.yaml)
- Dashboard: [sift-dashboard.yaml](./sift-dashboard.yaml)

See [docs/OPS.md](../../docs/OPS.md#monitoring-grafana--prometheus) for Cloudflare hostname setup.
