package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/amxrac/mpaas/api/internal/caddy"
	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/docker"
	"github.com/amxrac/mpaas/api/internal/handler"
	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/amxrac/mpaas/api/internal/service"
	"github.com/amxrac/mpaas/api/internal/stream"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	stream := stream.NewHub()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		log.Fatalf("connect docker: %v", err)
	}

	caddyAdminURL := os.Getenv("CADDY_ADMIN_URL")
	caddyClient := caddy.NewClient(caddyAdminURL)
	service := service.NewService(database, stream, dockerClient, caddyClient)
	deploymentHandler := handler.NewDeploymentHandler(service, database, stream)

	h := handler.NewHandler(deploymentHandler, database)

	port := os.Getenv("PORT")
	go func() {
		log.Printf("server running on  %s", port)
		err := h.Listen(port)
		if err != nil {
			log.Fatalf("connect docker: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}
