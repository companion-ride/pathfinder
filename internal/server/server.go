package server

import (
	"fmt"
	"log"
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
	port      int
	db        database.Service
	doublegis *doublegis.Client
	cache        *cache.Cache
	mapAPIKey    string
	jwtSecretKey string
}

func NewServer() (*http.Server, int) {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 8080
		if os.Getenv("PORT") != "" {
			log.Printf("WARNING: invalid PORT %q, defaulting to %d", os.Getenv("PORT"), port)
		}
	}
	apiKey := os.Getenv("DOUBLE_GIS_API_KEY")
	redisAddr := os.Getenv("REDIS_ADDR")
	s := &Server{
		port:      port,
		db:        database.New(),
		doublegis: doublegis.NewClient(apiKey),
		cache:     cache.New(redisAddr),
		mapAPIKey:    apiKey,
		jwtSecretKey: os.Getenv("JWT_SECRET_KEY"),
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
