COMPOSE := docker compose -f deploy/docker-compose.yml

# Local-only credentials, the same defaults as the compose file.
export ADMIN_TOKEN ?= dev-admin-token
export DEV_API_KEY ?= tk_live_localdev

.PHONY: up down db test vet run-api run-worker run-scaler web-install web-dev web-dev-mock web-build

up:
	$(COMPOSE) up -d --build

# The scaler's workers aren't compose's own. Stop the scaler first so it doesn't
# replace them, then remove them so the network can go.
down:
	$(COMPOSE) stop scaler
	@ids=$$(docker ps -aq --filter label=tasklane.role=worker); [ -z "$$ids" ] || docker rm -f $$ids
	$(COMPOSE) down

db:
	$(COMPOSE) up -d --wait postgres

test:
	go test ./...

vet:
	go vet ./...

run-api: db
	go run . api

run-worker: db
	go run . worker

# Outside Docker the scaler only logs what it would do.
run-scaler: db
	go run . scaler

web-install:
	cd web && npm ci

# The dev server proxies /api to the api on :8080.
web-dev:
	cd web && npm run dev

web-dev-mock:
	cd web && npm run dev:mock

web-build:
	cd web && npm run build
