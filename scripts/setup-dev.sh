#!/usr/bin/env bash
# setup-dev.sh — Bootstrap local development environment for CapyClaw.
set -euo pipefail

echo "==> Checking dependencies..."
command -v go >/dev/null 2>&1 || { echo "ERROR: Go 1.23+ required"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "ERROR: Docker required"; exit 1; }
command -v node >/dev/null 2>&1 || { echo "WARNING: Node.js not found (required for frontend)"; }

echo "==> Starting infrastructure (docker compose)..."
docker compose -f deploy/docker/docker-compose.yml up -d postgres redis vault

echo "==> Waiting for PostgreSQL to be ready..."
until docker compose -f deploy/docker/docker-compose.yml exec -T postgres \
    pg_isready -U capyclaw >/dev/null 2>&1; do
  sleep 1
done

echo "==> Running database migrations..."
./scripts/migrate.sh up

echo "==> Seeding test data..."
./scripts/seed.sh

echo "==> Installing Go toolchain extras..."
go install github.com/air-verse/air@latest 2>/dev/null || true
go install golang.org/x/tools/cmd/goimports@latest 2>/dev/null || true

if command -v node >/dev/null 2>&1; then
  echo "==> Installing frontend dependencies..."
  (cd web && npm ci)
fi

echo ""
echo "==> Dev environment ready!"
echo "    Run 'make dev' to start the gateway with hot-reload."
echo "    Run 'cd web && npm run dev' to start the frontend."
