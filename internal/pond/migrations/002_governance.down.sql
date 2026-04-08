-- Rollback governance migration
DROP TABLE IF EXISTS usage_monthly;
ALTER TABLE tenants DROP COLUMN IF EXISTS suspended_at;
ALTER TABLE tenants DROP COLUMN IF EXISTS suspended_reason;
