.PHONY: all build test lint fmt clean dev docker-up docker-up-all docker-down migrate seed

# ── Variables ───────────────────────────────────────────────────────────────
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOFLAGS    := -ldflags="-X main.Version=$(VERSION)"
BINARY_DIR := bin

# ── Default ──────────────────────────────────────────────────────────────────
all: build

# ── Build ────────────────────────────────────────────────────────────────────
build: build-gateway build-capy

build-gateway:
	@mkdir -p $(BINARY_DIR)
	go build $(GOFLAGS) -o $(BINARY_DIR)/capyclaw-gateway ./cmd/gateway

build-capy:
	@mkdir -p $(BINARY_DIR)
	go build $(GOFLAGS) -o $(BINARY_DIR)/capy ./cmd/capy

# ── Test ─────────────────────────────────────────────────────────────────────
test:
	go test ./... -race -timeout 120s

test-unit:
	go test ./internal/... ./pkg/... -race -timeout 60s

test-integration:
	go test ./tests/integration/... -race -timeout 300s -tags integration

# ── Code quality ─────────────────────────────────────────────────────────────
lint:
	golangci-lint run ./...

fmt:
	gofmt -w -s .
	goimports -w .

vet:
	go vet ./...

# ── Dev environment ──────────────────────────────────────────────────────────
dev: docker-up
	air -c .air.toml

docker-up:
	docker compose -f deploy/docker/docker-compose.yml up -d postgres redis temporal temporal-ui vault

docker-up-all:
	docker compose -f deploy/docker/docker-compose.yml up -d

docker-down:
	docker compose -f deploy/docker/docker-compose.yml down

# ── Database ─────────────────────────────────────────────────────────────────
migrate:
	./scripts/migrate.sh up

migrate-down:
	./scripts/migrate.sh down

seed:
	./scripts/seed.sh

# ── Clean ────────────────────────────────────────────────────────────────────
clean:
	rm -rf $(BINARY_DIR)
	go clean -cache
