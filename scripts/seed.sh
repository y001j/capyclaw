#!/usr/bin/env bash
# seed.sh — Insert development seed data into the local database.
set -euo pipefail

DSN="${CAPYCLAW_POND_POSTGRES_DSN:-postgres://capyclaw:capyclaw_dev@localhost:18700/capyclaw}"

echo "==> Seeding development data..."
psql "$DSN" <<'SQL'
-- Default tenant for local development
INSERT INTO tenants (id, name, slug, status)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Local Development',
    'local',
    'active'
) ON CONFLICT (id) DO NOTHING;

-- Default admin user
INSERT INTO users (id, tenant_id, email, role)
VALUES (
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000001',
    'admin@localhost',
    'admin'
) ON CONFLICT (id) DO NOTHING;
SQL

echo "==> Seed complete."
