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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pathfinder/internal/configclient"
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
		slog.Error("could not create gateway register request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		slog.Error("could not register with api-gateway", "error", err)
		return
	}
	resp.Body.Close()
	slog.Info("registered with api-gateway", "service", _serviceName)
}

func deregisterFromGateway() {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, gatewayURL+"/internal/deregister?name="+_serviceName, nil)
	if err != nil {
		slog.Error("could not create gateway deregister request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		slog.Error("could not deregister from api-gateway", "error", err)
		return
	}
	resp.Body.Close()
	slog.Info("deregistered from api-gateway", "service", _serviceName)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := database.RunMigrations("migrations"); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Initialize pathfinder config with defaults
	pfConfig := server.NewPathfinderConfigLoader(server.DefaultPathfinderConfig())

	configURL := os.Getenv("CONFIG_SERVICE_URL")
	configKey := os.Getenv("CONFIG_SERVICE_API_KEY")
	ctx, cancel := context.WithCancel(context.Background())

	if configURL != "" && configKey != "" {
		cc := configclient.New(configURL, configKey, "pathfinder")
		raw, changed, err := cc.Fetch(context.Background())
		if err != nil {
			slog.Warn("config-service unavailable at startup, using defaults", "error", err)
		} else if changed {
			var cfg server.PathfinderConfig
			if err := json.Unmarshal(raw, &cfg); err == nil {
				pfConfig.Store(&cfg)
				slog.Info("loaded pathfinder config from config-service")
			}
		}

		cc.StartPolling(ctx, 30*time.Second, func(raw json.RawMessage) {
			var cfg server.PathfinderConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				slog.Error("failed to unmarshal pathfinder config", "error", err)
				return
			}
			pfConfig.Store(&cfg)
		})
	}

	srv, port := server.NewServer(pfConfig)

	registerWithGateway(port)

	done := make(chan bool, 1)
	go func() {
		sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		<-sigCtx.Done()
		slog.Info("shutting down gracefully, press Ctrl+C again to force")
		stop()
		cancel() // stop config polling
		deregisterFromGateway()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Error("server forced to shutdown", "error", err)
		}
		slog.Info("server exiting")
		done <- true
	}()

	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	<-done
	slog.Info("graceful shutdown complete")
}
