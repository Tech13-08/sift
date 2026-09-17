#!/usr/bin/env bash
# Install / upgrade kube-prometheus-stack + Sift exporters/dashboard into k3d.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NS=monitoring
RELEASE=sift-monitoring
VALUES="$ROOT/k8s/monitoring/values.yaml"

# Read GRAFANA_ADMIN_PASSWORD without sourcing .env (values like SMTP_FROM=Name <email> break bash).
if [ -z "${GRAFANA_ADMIN_PASSWORD:-}" ] && [ -f "$ROOT/.env" ]; then
  GRAFANA_ADMIN_PASSWORD="$(
    python3 - "$ROOT/.env" <<'PY'
import sys
from pathlib import Path
for line in Path(sys.argv[1]).read_text().splitlines():
    s = line.strip()
    if not s or s.startswith("#") or "=" not in s:
        continue
    k, _, v = s.partition("=")
    if k.strip() != "GRAFANA_ADMIN_PASSWORD":
        continue
    v = v.strip().strip('"').strip("'")
    print(v, end="")
    break
PY
  )"
fi

if [ -z "${GRAFANA_ADMIN_PASSWORD:-}" ]; then
  echo "ERROR: GRAFANA_ADMIN_PASSWORD is required. Add it to .env (see .env.example)." >&2
  exit 1
fi

export PATH="${HOME}/.local/bin:${PATH}"

if ! command -v helm >/dev/null 2>&1; then
  echo "helm not found. Install to ~/.local/bin:"
  echo "  curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | HELM_INSTALL_DIR=\$HOME/.local/bin USE_SUDO=false bash"
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl not found" >&2
  exit 1
fi

kubectl get ns "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS"

# Exporters run in monitoring but need DB credentials from default.
kubectl get secret sift-secrets -n default -o json \
  | python3 -c 'import json,sys; o=json.load(sys.stdin); o["metadata"]={"name":"sift-secrets","namespace":"monitoring"}; print(json.dumps(o))' \
  | kubectl apply -f -
kubectl get configmap sift-config -n default -o json \
  | python3 -c 'import json,sys; o=json.load(sys.stdin); o["metadata"]={"name":"sift-config","namespace":"monitoring"}; print(json.dumps(o))' \
  | kubectl apply -f -

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts 2>/dev/null || true
helm repo update prometheus-community

echo "Installing/upgrading $RELEASE in namespace $NS..."
helm upgrade --install "$RELEASE" prometheus-community/kube-prometheus-stack \
  --namespace "$NS" \
  --values "$VALUES" \
  --set-string "grafana.adminPassword=${GRAFANA_ADMIN_PASSWORD}" \
  --wait \
  --timeout 10m

echo "Applying Redis/Postgres exporters + Sift dashboard..."
kubectl apply -f "$ROOT/k8s/monitoring/exporters.yaml"
kubectl apply -f "$ROOT/k8s/monitoring/sift-dashboard.yaml"

echo ""
echo "Done. Grafana Ingress host: grafana-sift.falaktulsi.com"
echo "Local check: curl -sfI -H 'Host: grafana-sift.falaktulsi.com' http://localhost:8088/"
echo "Admin login (local only): admin / \$GRAFANA_ADMIN_PASSWORD from .env"
echo "Add Cloudflare public hostname grafana-sift.falaktulsi.com → http://traefik.kube-system.svc.cluster.local:80"
