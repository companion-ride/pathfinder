// Pathfinder API
//
//	@title			Pathfinder API
//	@version		1.0
//	@description	GIS routing service — builds driving routes via 2GIS and computes walking paths from a passenger to the nearest point on a route.
//	@host			localhost:8080
//	@BasePath		/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pathfinder/internal/database"
	"pathfinder/internal/server"
)

const _serviceName = "pathfinder"

var gatewayClient = &http.Client{Timeout: 5 * time.Second}

func registerWithGateway(port int) {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"name":         _serviceName,
		"address":      os.Getenv("SERVICE_HOST"),
		"port":         port,
		"health_check": "/health",
	})
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/internal/register", bytes.NewReader(body))
	if err != nil {
		log.Printf("could not create gateway register request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		log.Printf("could not register with api-gateway: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("registered with api-gateway as %s", _serviceName)
}

func deregisterFromGateway() {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, gatewayURL+"/internal/deregister?name="+_serviceName, nil)
	if err != nil {
		log.Printf("could not create gateway deregister request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		log.Printf("could not deregister from api-gateway: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("deregistered from api-gateway: %s", _serviceName)
}

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	log.Println("shutting down gracefully, press Ctrl+C again to force")
	stop()

	deregisterFromGateway()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown with error: %v", err)
	}

	log.Println("Server exiting")
	done <- true
}

func main() {
	if err := database.RunMigrations("migrations"); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	srv, port := server.NewServer()

	registerWithGateway(port)

	done := make(chan bool, 1)
	go gracefulShutdown(srv, done)

	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	// Wait for the graceful shutdown to complete
	<-done
	log.Println("Graceful shutdown complete.")
}
