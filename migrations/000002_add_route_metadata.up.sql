ALTER TABLE routes
    ADD COLUMN total_distance INTEGER,
    ADD COLUMN total_duration INTEGER,
    ADD COLUMN waypoints     JSONB,
    ADD COLUMN maneuvers     JSONB;
