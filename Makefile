# Stateless app services (leave redis running)
STATELESS := ingestor collector predictor announcer evaluator

# Build all stateless images (no cache so you get fresh builds)
build:
	docker compose build --no-cache $(STATELESS)

# Build then deploy stateless only: new containers from new images, redis unchanged
deploy-stateless: build
	docker compose up -d --force-recreate $(STATELESS)

# Same as deploy-stateless but build with cache (faster when only one app changed)
deploy-stateless-fast:
	docker compose build $(STATELESS)
	docker compose up -d --force-recreate $(STATELESS)

.PHONY: build deploy-stateless deploy-stateless-fast
