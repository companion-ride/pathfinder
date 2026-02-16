package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/joho/godotenv/autoload"
)

type RouteMetadata struct {
	TotalDistance  int
	TotalDuration int
	StartLon      float64
	StartLat      float64
	EndLon        float64
	EndLat        float64
	Maneuvers     json.RawMessage
}

type RouteRow struct {
	ID            string
	Geometry      string // WKT
	TotalDistance  int
	TotalDuration int
	Maneuvers     json.RawMessage
}

// Service represents a service that interacts with a database.
type Service interface {
	// Health returns a map of health status information.
	Health() map[string]string

	// Close terminates the database connection.
	Close() error

	// SaveRoute stores a WKT LINESTRING geometry with metadata and returns the route ID.
	SaveRoute(ctx context.Context, wktLinestring string, meta RouteMetadata) (string, error)

	// GetRouteGeometry retrieves the WKT LINESTRING for a given route ID.
	GetRouteGeometry(ctx context.Context, routeID string) (string, error)

	// GetRouteByID retrieves the full route row for a given route ID.
	GetRouteByID(ctx context.Context, routeID string) (*RouteRow, error)
}

type service struct {
	db *sql.DB
}

var (
	database   = os.Getenv("DB_DATABASE")
	password   = os.Getenv("DB_PASSWORD")
	username   = os.Getenv("DB_USERNAME")
	port       = os.Getenv("DB_PORT")
	host       = os.Getenv("DB_HOST")
	schema     = os.Getenv("DB_SCHEMA")
	dbInstance *service
)

func New() Service {
	// Reuse Connection
	if dbInstance != nil {
		return dbInstance
	}
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&search_path=%s", username, password, host, port, database, schema)
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatal(err)
	}
	dbInstance = &service{
		db: db,
	}
	return dbInstance
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)

	// Ping the database
	err := s.db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Fatalf("db down: %v", err) // Log the error and terminate the program
		return stats
	}

	// Database is up, add more statistics
	stats["status"] = "up"
	stats["message"] = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := s.db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats["message"] = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

// Close closes the database connection.
// It logs a message indicating the disconnection from the specific database.
// If the connection is successfully closed, it returns nil.
// If an error occurs while closing the connection, it returns the error.
func (s *service) Close() error {
	log.Printf("Disconnected from database: %s", database)
	return s.db.Close()
}

func (s *service) SaveRoute(ctx context.Context, wktLinestring string, meta RouteMetadata) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO routes (geometry, total_distance, total_duration, start_point, end_point, maneuvers)
		 VALUES (ST_GeomFromText($1, 4326), $2, $3,
		         ST_SetSRID(ST_MakePoint($4, $5), 4326),
		         ST_SetSRID(ST_MakePoint($6, $7), 4326),
		         $8)
		 RETURNING id`,
		wktLinestring, meta.TotalDistance, meta.TotalDuration,
		meta.StartLon, meta.StartLat,
		meta.EndLon, meta.EndLat,
		meta.Maneuvers,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("inserting route: %w", err)
	}
	return id, nil
}

func (s *service) GetRouteGeometry(ctx context.Context, routeID string) (string, error) {
	var wkt string
	err := s.db.QueryRowContext(ctx,
		"SELECT ST_AsText(geometry) FROM routes WHERE id = $1",
		routeID,
	).Scan(&wkt)
	if err != nil {
		return "", fmt.Errorf("getting route geometry: %w", err)
	}
	return wkt, nil
}

func (s *service) GetRouteByID(ctx context.Context, routeID string) (*RouteRow, error) {
	var row RouteRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, ST_AsText(geometry), total_distance, total_duration, maneuvers
		 FROM routes WHERE id = $1`,
		routeID,
	).Scan(&row.ID, &row.Geometry, &row.TotalDistance, &row.TotalDuration, &row.Maneuvers)
	if err != nil {
		return nil, err
	}
	return &row, nil
}
