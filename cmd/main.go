package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/amxrac/mpaas/internal/api"
	"github.com/amxrac/mpaas/internal/caddy"
	"github.com/amxrac/mpaas/internal/db"
	"github.com/amxrac/mpaas/internal/docker"
	"github.com/amxrac/mpaas/internal/models"
	"github.com/amxrac/mpaas/internal/service"
	"github.com/amxrac/mpaas/internal/stream"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.ConnectDB("mpaas")
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

	caddyClient := caddy.NewClient("http://localhost:2019")
	service := service.NewService(database, stream, dockerClient, caddyClient)
	deploymentHandler := api.NewDeploymentHandler(service, database, stream)

	h := api.NewHandler(deploymentHandler, database)

	go func() {
		log.Printf("server running on :8000")
		err := h.Listen(":8000")
		if err != nil {
			log.Fatalf("connect docker: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}
