-- WARNING: This migration is irreversible. All forecast snapshot data will be
-- permanently deleted. Take a backup before running in any environment that
-- holds data you want to keep.
DROP TABLE IF EXISTS forecast_snapshots CASCADE;
