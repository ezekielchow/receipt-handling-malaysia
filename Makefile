.PHONY: help ollama-build ollama-deploy ollama-pull-model ollama-logs

# ==== Load .env into Make environment ====
ifneq (,$(wildcard .env))
    include .env
    export
endif

# ==== Default fallback values (if missing in .env) ====
PROJECT_ID ?= your-gcp-project-id
REGION ?= asia-southeast1
SERVICE_NAME ?= ollama-mistral
IMAGE_NAME ?= ollama-mistral
DOCKER_CONTEXT ?= ./docker/ollama

# ==== Help ====
help:
	@echo "Usage: make <command>"
	@echo ""
	@echo "Ollama Commands:"
	@echo "  ollama-build         Build the Docker image for Ollama using Cloud Build"
	@echo "  ollama-deploy        Deploy the Ollama container to Cloud Run"
	@echo "  ollama-pull-model    Pull the Mistral model after deployment"
	@echo "  ollama-logs          Show logs from the deployed Ollama service"

# ==== Build Image with Cloud Build ====
ollama-build:
	@echo ">> Using PROJECT_ID=$(PROJECT_ID)"
	@cd $(DOCKER_CONTEXT) && \
		env $$(cat ../../.env | xargs) \
		gcloud builds submit . --tag gcr.io/$(PROJECT_ID)/$(IMAGE_NAME)

# ==== Deploy to Cloud Run ====
ollama-deploy:
	env $$(cat .env | xargs) \
	gcloud run deploy $(SERVICE_NAME) \
		--image gcr.io/$(PROJECT_ID)/$(IMAGE_NAME) \
		--platform managed \
		--region $(REGION) \
		--allow-unauthenticated \
        --memory 24Gi \
        --cpu 6 \
		--timeout 900s

# ==== Pull model after deploy (assumes public endpoint) ====
ollama-pull-model:
	curl -X POST https://$(SERVICE_NAME)-$(REGION)-a.run.app/api/pull \
		-H "Content-Type: application/json" \
		-d '{"name": "mistral"}'

# ==== View Logs ====
ollama-logs:
	gcloud logging read "resource.type=cloud_run_revision AND resource.labels.service_name=$(SERVICE_NAME)" \
		--project=$(PROJECT_ID) \
		--limit=50 --order=desc
