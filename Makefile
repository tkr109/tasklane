COMPOSE := docker compose -f deploy/docker-compose.yml

# Local-only credentials, the same defaults as the compose file.
export ADMIN_TOKEN ?= dev-admin-token
export DEV_API_KEY ?= tk_live_localdev

.PHONY: up down db test vet run-api run-worker

up:
	$(COMPOSE) up -d --build

down:
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
