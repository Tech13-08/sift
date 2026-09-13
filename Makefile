K8S_PATH = ./k8s/manifests
SERVICES_PATH = ./services
CLUSTER = sift-cluster
APPS_DIR = $(K8S_PATH)/apps

TAG := $(shell date +%s)

apply-config:
	kubectl apply -f $(K8S_PATH)/config/configmap.yaml
	kubectl apply -f $(K8S_PATH)/config/ingress.yaml
	@if [ -f $(K8S_PATH)/config/secrets.local.yaml ]; then \
		echo "Using $(K8S_PATH)/config/secrets.local.yaml"; \
		kubectl apply -f $(K8S_PATH)/config/secrets.local.yaml; \
	else \
		echo "Using placeholder secrets.yaml — copy secrets.local.yaml.example for real credentials"; \
		kubectl apply -f $(K8S_PATH)/config/secrets.yaml; \
	fi

deploy-all: apply-config
	kubectl apply -f $(K8S_PATH)/infra/
	kubectl apply -k $(APPS_DIR)

# Deletes only resources from this repo's manifests, not the whole cluster.
nuke:
	@echo "--- Removing Sift apps, infra, and config ---"
	-kubectl delete -k $(APPS_DIR) --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/infra/ --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/configmap.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/ingress.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/secrets.yaml --ignore-not-found
	-kubectl delete -f $(K8S_PATH)/config/secrets.local.yaml --ignore-not-found

start-world:
	@echo "--- Deploying Config and Infra ---"
	$(MAKE) apply-config
	kubectl apply -f $(K8S_PATH)/infra/
	@echo "--- Waiting for Postgres to initialize init.sql ---"
	kubectl wait --for=condition=ready pod -l app=postgres --timeout=60s
	@echo "--- Building Images and Deploying Apps ---"
	$(MAKE) sync-all
	@echo "--- COMPLETE: System is live with new schema ---"

reset-world:
	$(MAKE) nuke
	$(MAKE) start-world

clean-apps:
	kubectl delete -k $(APPS_DIR)

deploy-apps:
	kubectl apply -k $(APPS_DIR)

sync-auth:
	@echo "Syncing Auth Service with tag: $(TAG)"
	docker build -t sift-auth:$(TAG) $(SERVICES_PATH)/auth-service
	k3d image import sift-auth:$(TAG) -c $(CLUSTER)
	(cd $(APPS_DIR) && kustomize edit set image sift-auth=sift-auth:$(TAG))
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

sync-all:
	@echo "Starting Full Sync with tag: $(TAG)"
	docker build -t sift-auth:$(TAG) $(SERVICES_PATH)/auth-service
	docker build -t sift-summarizer:$(TAG) $(SERVICES_PATH)/summarizer-service
	docker build -t sift-ingestion:$(TAG) $(SERVICES_PATH)/ingestion-service
	k3d image import sift-auth:$(TAG) sift-summarizer:$(TAG) sift-ingestion:$(TAG) -c $(CLUSTER)

	(cd $(APPS_DIR) && \
		kustomize edit set image sift-auth=sift-auth:$(TAG) && \
		kustomize edit set image sift-summarizer=sift-summarizer:$(TAG) && \
		kustomize edit set image sift-ingestion=sift-ingestion:$(TAG))

	kubectl apply -k $(APPS_DIR)
	@echo "All services synchronized."

watch-pods:
	watch -n 1 kubectl get pods
