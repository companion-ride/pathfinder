ALTER TABLE routes
    DROP COLUMN IF EXISTS total_distance,
    DROP COLUMN IF EXISTS total_duration,
    DROP COLUMN IF EXISTS waypoints,
    DROP COLUMN IF EXISTS maneuvers;
