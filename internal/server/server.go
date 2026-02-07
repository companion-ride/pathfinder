package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"pathfinder/internal/database"
	"pathfinder/internal/doublegis"
)

type Server struct {
	port      int
	db        database.Service
	doublegis *doublegis.Client
	mapAPIKey string
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	apiKey := os.Getenv("DOUBLE_GIS_API_KEY")
	NewServer := &Server{
		port:      port,
		db:        database.New(),
		doublegis: doublegis.NewClient(apiKey),
		mapAPIKey: apiKey,
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
