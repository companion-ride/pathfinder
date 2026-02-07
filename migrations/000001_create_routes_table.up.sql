CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS routes (
    id         SERIAL PRIMARY KEY,
    geometry   geometry(LINESTRING, 4326) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_routes_geometry ON routes USING GIST (geometry);
