DROP INDEX IF EXISTS idx_routes_end_point;
DROP INDEX IF EXISTS idx_routes_start_point;

ALTER TABLE routes
  DROP COLUMN IF EXISTS end_point,
  DROP COLUMN IF EXISTS start_point,
  ADD COLUMN waypoints JSONB;
