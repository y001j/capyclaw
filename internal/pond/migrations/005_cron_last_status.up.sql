-- Add last_status column to cron_jobs table for tracking execution results
ALTER TABLE cron_jobs ADD COLUMN IF NOT EXISTS last_status VARCHAR(50);
