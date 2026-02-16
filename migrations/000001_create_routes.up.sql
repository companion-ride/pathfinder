CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS routes (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    geometry       geometry(LINESTRING, 4326) NOT NULL,
    total_distance INTEGER,
    total_duration INTEGER,
    start_point    geometry(POINT, 4326),
    end_point      geometry(POINT, 4326),
    maneuvers      JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_routes_geometry    ON routes USING GIST (geometry);
CREATE INDEX idx_routes_start_point ON routes USING GIST (start_point);
CREATE INDEX idx_routes_end_point   ON routes USING GIST (end_point);
