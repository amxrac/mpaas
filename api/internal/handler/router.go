package handler

import (
	"encoding/json"
	"net/http"

	"github.com/amxrac/mpaas/api/internal/db"
)

type DeploymentHandler interface {
	Create(http.ResponseWriter, *http.Request)
	List(http.ResponseWriter, *http.Request)
	Get(http.ResponseWriter, *http.Request)
	Stop(http.ResponseWriter, *http.Request)
	StreamLogs(http.ResponseWriter, *http.Request)
}

type Handler struct {
	Deployment DeploymentHandler
	db         *db.DB
}

func NewHandler(deployment DeploymentHandler, database *db.DB) *Handler {
	return &Handler{
		Deployment: deployment,
		db:         database,
	}
}

func Setup(h *Handler, db *db.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health(db))
	mux.HandleFunc("POST /deployments", h.Deployment.Create)
	mux.HandleFunc("GET /deployments", h.Deployment.List)
	mux.HandleFunc("GET /deployments/{id}", h.Deployment.Get)
	mux.HandleFunc("DELETE /deployments/{id}", h.Deployment.Stop)
	mux.HandleFunc("GET /deployments/{id}/logs/stream", h.Deployment.StreamLogs)
	return enableCORS(mux)
}

func health(db *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "database unavailable",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	}
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

func (h *Handler) Listen(addr string) error {
	return http.ListenAndServe(addr, Setup(h, h.db))
}
