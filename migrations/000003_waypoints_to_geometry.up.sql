ALTER TABLE routes
  DROP COLUMN IF EXISTS waypoints,
  ADD COLUMN start_point geometry(POINT, 4326),
  ADD COLUMN end_point   geometry(POINT, 4326);

CREATE INDEX idx_routes_start_point ON routes USING GIST (start_point);
CREATE INDEX idx_routes_end_point   ON routes USING GIST (end_point);
