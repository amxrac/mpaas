package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amxrac/mpaas/api/internal/caddy"
	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/docker"
	"github.com/amxrac/mpaas/api/internal/handler"
	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/amxrac/mpaas/api/internal/service"
	"github.com/amxrac/mpaas/api/internal/stream"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	port := os.Getenv("PORT")
	if port == "" {
		log.Fatalf("PORT must be set (e.g. :8000)")
	}

	caddyAdminURL := os.Getenv("CADDY_ADMIN_URL")
	if caddyAdminURL == "" {
		log.Fatalf("CADDY_ADMIN_URL must be set (e.g. http://caddy:2019)")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "mpaas"
	}

	database, err := db.ConnectDB(dbPath)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}

	err = database.Migrate(&models.Deployment{}, &models.LogEntry{})
	if err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	hub := stream.NewHub()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		log.Fatalf("connect docker: %v", err)
	}

	caddyClient := caddy.NewClient(caddyAdminURL)
	svc := service.NewService(database, hub, dockerClient, caddyClient)
	deploymentHandler := handler.NewDeploymentHandler(svc, database, hub)
	h := handler.NewHandler(deploymentHandler, database, dockerClient)

	srv := h.NewServer(port)

	go func() {
		log.Printf("server running on %s", srv.Addr)
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = srv.Shutdown(shutdownCtx)
	if err != nil {
		log.Printf("forcing shutdown; graceful shutdown interrupted. %v", err)
		_ = srv.Close()
	}
}
