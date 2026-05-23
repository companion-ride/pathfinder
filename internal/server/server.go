package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"pathfinder/internal/cache"
	"pathfinder/internal/database"
	"pathfinder/internal/doublegis"
)

type Server struct {
	port         int
	db           database.Service
	doublegis    *doublegis.Client
	cache        *cache.Cache
	mapAPIKey    string
	jwtSecretKey string
	pfConfig     *PathfinderConfigLoader
}

func NewServer(pfConfig *PathfinderConfigLoader) (*http.Server, int) {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 8080
		if os.Getenv("PORT") != "" {
			slog.Warn("invalid PORT, using default", "port_value", os.Getenv("PORT"), "port", port)
		}
	}
	// Validate critical environment variables
	jwtSecret := os.Getenv("JWT_SECRET_KEY")
	if jwtSecret == "" {
		slog.Error("missing required environment variable", "variable", "JWT_SECRET_KEY")
		os.Exit(1)
	}
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME"} {
		if os.Getenv(key) == "" {
			slog.Error("missing required environment variable", "variable", key)
			os.Exit(1)
		}
	}
	apiKeys, err := parseAPIKeys()
	if err != nil {
		slog.Error("missing required environment variable", "variable", "DOUBLE_GIS_API_KEYS", "error", err)
		os.Exit(1)
	}
	slog.Info("loaded 2gis api keys", "count", len(apiKeys))

	redisAddr := os.Getenv("REDIS_ADDR")
	s := &Server{
		port: port,
		db:   database.New(),
		doublegis: doublegis.NewClient(apiKeys, func() doublegis.RouteParams {
			cfg := pfConfig.Get()
			return doublegis.RouteParams{
				RouteMode:   cfg.RouteMode,
				TrafficMode: cfg.TrafficMode,
				Locale:      cfg.Locale,
				Timeout:     time.Duration(cfg.RequestTimeoutSeconds) * time.Second,
			}
		}),
		cache:        cache.New(redisAddr),
		mapAPIKey:    apiKeys[0],
		jwtSecretKey: jwtSecret,
		pfConfig:     pfConfig,
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server, port
}
