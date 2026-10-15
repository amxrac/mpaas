package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/docker"
)

type DeploymentHandler interface {
	Create(http.ResponseWriter, *http.Request)
	List(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	Stop(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
	StreamLogs(http.ResponseWriter, *http.Request)
}

type Handler struct {
	Deployment   DeploymentHandler
	database     *db.DB
	dockerClient *docker.Client
}

func NewHandler(deployment DeploymentHandler, database *db.DB, dockerClient *docker.Client) *Handler {
	return &Handler{
		Deployment:   deployment,
		database:     database,
		dockerClient: dockerClient,
	}
}

func Setup(h *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", health(h.database, h.dockerClient))
	mux.HandleFunc("POST /api/deployments", h.Deployment.Create)
	mux.HandleFunc("GET /api/deployments", h.Deployment.List)
	mux.HandleFunc("GET /api/deployments/{id}", h.Deployment.Get)
	mux.HandleFunc("POST /api/deployments/{id}/stop", h.Deployment.Stop)
	mux.HandleFunc("DELETE /api/deployments/{id}", h.Deployment.Delete)
	mux.HandleFunc("GET /api/deployments/{id}/logs/stream", h.Deployment.StreamLogs)
	return enableCORS(mux)
}

func health(database *db.DB, dockerClient *docker.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := database.Ping()
		if err != nil {
			writeHealth(w, http.StatusServiceUnavailable, "error", "database unavailable")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		err = dockerClient.Ping(ctx)
		if err != nil {
			writeHealth(w, http.StatusServiceUnavailable, "error", "docker unavailable")
			return
		}

		writeHealth(w, http.StatusOK, "ready", "")
	}
}

func writeHealth(w http.ResponseWriter, status int, state, message string) {
	body := map[string]string{"status": state}
	if message != "" {
		body["message"] = message
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func enableCORS(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		w.Header().Add("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func (h *Handler) NewServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           Setup(h),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}
