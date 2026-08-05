package api

import (
	"net/http"

	"github.com/amxrac/mpaas/internal/db"
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
		err := db.Ping()
		if err != nil {
			http.Error(w, `{"status": "error", "message": "database unavailable"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
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
