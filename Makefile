K8S_PATH = ./k8s/manifests
SERVICES_PATH = ./services
CLUSTER = sift-cluster
APPS_DIR = $(K8S_PATH)/apps
# Host port for Traefik ingress (http://localhost:8088).
INGRESS_HOST_PORT ?= 8088

TAG := $(shell date +%s)

.PHONY: create-cluster delete-cluster apply-config apply-infra apply-postgres-init \
	deploy-all start-world reset-world nuke clean-apps deploy-apps \
	sync-branding sync-auth sync-web sync-ingestion sync-summarizer sync-discord sync-all watch-pods \
	sync-secrets-from-env migrate-db-from-compose stop-compose-prod promote-k3d-prod \
	pubsub-point-at-public ensure-up install-autostart install-ollama-autostart install-monitoring \
	backup-db

# Create local k3d cluster if missing. Maps host :8088 → Traefik :80.
create-cluster:
	@if k3d cluster list | awk 'NR>1 {print $$1}' | grep -qx '$(CLUSTER)'; then \
		echo "k3d cluster $(CLUSTER) already exists"; \
		k3d cluster start $(CLUSTER) || true; \
	else \
		echo "Creating k3d cluster $(CLUSTER) (ingress on localhost:$(INGRESS_HOST_PORT))"; \
		k3d cluster create $(CLUSTER) \
			--agents 0 \
			-p "$(INGRESS_HOST_PORT):80@loadbalancer"; \
	fi
	kubectl config use-context k3d-$(CLUSTER)

delete-cluster:
	k3d cluster delete $(CLUSTER)

apply-postgres-init:
	kubectl create configmap postgres-init-sql \
		--from-file=init.sql=./infra/postgres/init.sql \
		--dry-run=client -o yaml | kubectl apply -f -

apply-config:
	kubectl apply -f $(K8S_PATH)/config/configmap.yaml
	kubectl apply -f $(K8S_PATH)/config/ingress.yaml
	@if [ -f $(K8S_PATH)/config/secrets.local.yaml ]; then \
		echo "Using $(K8S_PATH)/config/secrets.local.yaml"; \
		if ! grep -q 'TOKEN_ENCRYPTION_KEY:' $(K8S_PATH)/config/secrets.local.yaml; then \
			echo "ERROR: secrets.local.yaml is missing TOKEN_ENCRYPTION_KEY - copy from secrets.local.yaml.example"; \
			exit 1; \
		fi; \
		if ! grep -q 'CF_TUNNEL_TOKEN:' $(K8S_PATH)/config/secrets.local.yaml; then \
			echo "ERROR: secrets.local.yaml is missing CF_TUNNEL_TOKEN - run make sync-secrets-from-env"; \
			exit 1; \
		fi; \
		kubectl apply -f $(K8S_PATH)/config/secrets.local.yaml; \
	else \
		echo "Using placeholder secrets.yaml - copy secrets.local.yaml.example for real credentials"; \
		kubectl apply -f $(K8S_PATH)/config/secrets.yaml; \
	fi

# Copy .env secrets into secrets.local.yaml (keeps TOKEN_ENCRYPTION_KEY in sync for DB migrate).
sync-secrets-from-env:
	python3 ./scripts/sync-env-to-k8s-secrets.py
	$(MAKE) apply-config

# Dump Compose sift-db → restore into k3d Postgres PVC.
migrate-db-from-compose:
	chmod +x ./scripts/migrate-compose-db-to-k3d.sh
	./scripts/migrate-compose-db-to-k3d.sh

# Stop Compose “prod” containers so k3d owns traffic/data. Leaves volumes intact as backup.
stop-compose-prod:
	docker compose stop tunnel web-service auth-service ingestion-service summarizer-service discord-service hookdeck db redis || true
	@echo "Compose app/db/tunnel stopped. Volume sift_postgres_data kept as backup."
	@echo "Cloudflare hostname service MUST be: http://traefik.kube-system.svc.cluster.local:80"

# Full cutover: sync secrets → migrate DB → deploy tunnel/config → stop Compose.
promote-k3d-prod: sync-secrets-from-env
	kubectl apply -f $(K8S_PATH)/config/configmap.yaml
	kubectl apply -f $(K8S_PATH)/config/ingress.yaml
	kubectl apply -k $(APPS_DIR)
	$(MAKE) migrate-db-from-compose
	$(MAKE) stop-compose-prod
	@echo ""
	@echo "=== Manual Cloudflare step ==="
	@echo "Zero Trust → your tunnel → Public Hostname for sift.falaktulsi.com"
	@echo "  Service = HTTP http://traefik.kube-system.svc.cluster.local:80"
	@echo "Then: curl -sfI https://sift.falaktulsi.com/"
	@echo "Then: make pubsub-point-at-public  # Gmail → domain webhook (no Hookdeck)"

# Point Gmail Pub/Sub push at https://sift…/webhooks/gmail?token=WEBHOOK_SECRET
pubsub-point-at-public:
	chmod +x ./scripts/pubsub-point-at-public.sh
	./scripts/pubsub-point-at-public.sh

# Gzipped pg_dump into ./backups (keeps last 5). Low disk; use --force to ignore age skip.
backup-db:
	chmod +x ./scripts/backup-db.sh
	./scripts/backup-db.sh --force

# Wait for Docker, start k3d cluster, wait for Sift pods (also run by Windows boot task).
ensure-up:
	chmod +x ./scripts/ensure-sift-up.sh
	./scripts/ensure-sift-up.sh

# Register Windows "Sift Ensure Up" AtLogOn task (Ollama + Docker wait + ensure-up).
install-autostart:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$$(wslpath -w $(CURDIR)/scripts/windows/Install-SiftAutostart.ps1)"

# Register Ollama AtStartup (needs one UAC approval; runs before Windows sign-in).
install-ollama-autostart:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$$(wslpath -w $(CURDIR)/scripts/windows/Request-OllamaBootInstall.ps1)"

apply-infra: apply-postgres-init
	kubectl apply -f $(K8S_PATH)/infra/

deploy-all: apply-config apply-infra
	kubectl apply -k $(APPS_DIR)

# Deletes only resources from this repo's manifests, not the whole cluster.
nuke:
	@echo "--- Removing Sift apps, infra, and config ---"
	-kubectl delete -k $(APPS_DIR) --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/infra/ --ignore-not-found
	-kubectl delete configmap postgres-init-sql --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/configmap.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/ingress.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/secrets.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/secrets.local.yaml --ignore-not-found

# Full local bring-up: cluster → config/infra → images → apps.
start-world: create-cluster
	@echo "--- Deploying Config and Infra ---"
	$(MAKE) apply-config
	$(MAKE) apply-infra
	@echo "--- Waiting for Postgres ---"
	kubectl wait --for=condition=ready pod -l app=postgres --timeout=180s
	@echo "--- Building Images and Deploying Apps ---"
	$(MAKE) sync-all
	@echo "--- COMPLETE: http://localhost:$(INGRESS_HOST_PORT) ---"

reset-world:
	$(MAKE) nuke
	$(MAKE) start-world

clean-apps:
	kubectl delete -k $(APPS_DIR)

deploy-apps:
	kubectl apply -k $(APPS_DIR)

# Regenerate CSS/JSON/TS/SVG from branding/branding.yaml
sync-branding:
	node scripts/sync-branding.js

sync-auth: sync-branding
	@echo "Syncing Auth Service with tag: $(TAG)"
	docker build -t sift-auth:$(TAG) $(SERVICES_PATH)/auth-service
	k3d image import sift-auth:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-auth=sift-auth:$(TAG))
	kubectl apply -k $(APPS_DIR)

sync-web: sync-branding
	@echo "Syncing Web Service with tag: $(TAG)"
	docker build -t sift-web:$(TAG) --build-arg AUTH_INTERNAL_URL=http://auth-service:3000 $(SERVICES_PATH)/web-service
	k3d image import sift-web:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-web=sift-web:$(TAG))
	kubectl apply -k $(APPS_DIR)

sync-summarizer:
	@echo "Syncing Summarizer Service with tag: $(TAG)"
	docker build -t sift-summarizer:$(TAG) $(SERVICES_PATH)/summarizer-service
	k3d image import sift-summarizer:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-summarizer=sift-summarizer:$(TAG))
	kubectl apply -k $(APPS_DIR)

sync-ingestion:
	@echo "Syncing Ingestion Service with tag: $(TAG)"
	docker build -t sift-ingestion:$(TAG) $(SERVICES_PATH)/ingestion-service
	k3d image import sift-ingestion:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-ingestion=sift-ingestion:$(TAG))
	kubectl apply -k $(APPS_DIR)

sync-discord:
	@echo "Syncing Discord Service with tag: $(TAG)"
	docker build -t sift-discord:$(TAG) $(SERVICES_PATH)/discord-service
	k3d image import sift-discord:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-discord=sift-discord:$(TAG))
	kubectl apply -k $(APPS_DIR)

sync-all: sync-branding
	@echo "Starting Full Sync with tag: $(TAG)"
	docker build -t sift-auth:$(TAG) $(SERVICES_PATH)/auth-service
	docker build -t sift-web:$(TAG) --build-arg AUTH_INTERNAL_URL=http://auth-service:3000 $(SERVICES_PATH)/web-service
	docker build -t sift-summarizer:$(TAG) $(SERVICES_PATH)/summarizer-service
	docker build -t sift-ingestion:$(TAG) $(SERVICES_PATH)/ingestion-service
	docker build -t sift-discord:$(TAG) $(SERVICES_PATH)/discord-service
	k3d image import \
		sift-auth:$(TAG) \
		sift-web:$(TAG) \
		sift-summarizer:$(TAG) \
		sift-ingestion:$(TAG) \
		sift-discord:$(TAG) \
		-c $(CLUSTER)
	(cd $(APPS_DIR) && \
		kustomize edit set image sift-auth=sift-auth:$(TAG) && \
		kustomize edit set image sift-web=sift-web:$(TAG) && \
		kustomize edit set image sift-summarizer=sift-summarizer:$(TAG) && \
		kustomize edit set image sift-ingestion=sift-ingestion:$(TAG) && \
		kustomize edit set image sift-discord=sift-discord:$(TAG))
	kubectl apply -k $(APPS_DIR)
	@echo "All services synchronized."

watch-pods:
	watch -n 1 kubectl get pods

# Prometheus + Grafana (public Viewer at grafana-sift.falaktulsi.com). See docs/OPS.md.
install-monitoring:
	chmod +x scripts/install-monitoring.sh
	./scripts/install-monitoring.sh
