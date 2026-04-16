-- WARNING: This migration is irreversible. All observation snapshot data will
-- be permanently deleted. Take a backup before running in any environment that
-- holds data you want to keep.
DROP TABLE IF EXISTS observation_snapshots CASCADE;
