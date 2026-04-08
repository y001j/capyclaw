#!/usr/bin/env bash
# migrate.sh — Run database migrations via the gateway binary.
# Usage: ./scripts/migrate.sh [up|down]
set -euo pipefail

DIRECTION="${1:-up}"
DSN="${CAPYCLAW_POND_POSTGRES_DSN:-postgres://capyclaw:capyclaw_dev@localhost:18700/capyclaw}"

echo "==> Running migrations ($DIRECTION) against $DSN..."
go run ./cmd/gateway migrate --direction "$DIRECTION" --dsn "$DSN"
echo "==> Migration complete."
