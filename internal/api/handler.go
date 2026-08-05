package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/amxrac/mpaas/internal/db"
	"github.com/amxrac/mpaas/internal/service"
	"github.com/amxrac/mpaas/internal/stream"
	"gorm.io/gorm"
)

type deploymentHandler struct {
	service *service.Service
	db      *db.DB
	streams *stream.Hub
}

type apiResponse struct {
	Success bool `json:"success"`
	Data    any  `json:"data,omitempty"`
}

type createDeploymentRequest struct {
	GithubURL string `json:"github_url"`
}

func NewDeploymentHandler(
	service *service.Service,
	db *db.DB,
	streams *stream.Hub,
) *deploymentHandler {
	return &deploymentHandler{
		service: service,
		db:      db,
		streams: streams,
	}
}

func (h *deploymentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createDeploymentRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		badRequest(w, "invalid request body")
		return
	}

	githubURL := strings.TrimSpace(req.GithubURL)
	_, _, _, err = service.ParseGitHubRepoURL(githubURL)
	if err != nil {
		badRequest(w, "invalid request body")
		return
	}

	_, err = h.service.Deploy(r.Context(), githubURL)
	if err != nil {
		internalError(w, "failed to create deployment")
		return
	}

	accepted(w, "deployment created successfully")
}

func (h *deploymentHandler) List(w http.ResponseWriter, r *http.Request) {
	_, err := h.db.ListDeployments(r.Context())
	if err != nil {
		internalError(w, "failed to fetch deployments")
		return
	}

	ok(w, "deployments fetched successfully")
}

func (h *deploymentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.db.GetDeploymentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found")
			return
		}

		internalError(w, "failed to fetch deployment")
		return
	}

	ok(w, "deployment fetched successfully")

}

func (h *deploymentHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.service.Stop(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found")
			return
		}

		internalError(w, "failed to stop deployment")
		return
	}

	ok(w, "deployment stopped successfully")
}

func (h *deploymentHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.db.GetDeploymentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found")
			return
		}

		internalError(w, "failed to stop deployment")
		return
	}

	stream := h.streams.Subscribe(id)
	defer h.streams.Unsubscribe(id, stream)

	flusher, ok := w.(http.Flusher)
	if !ok {
		internalError(w, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-stream:
			if !open {
				return
			}

			payload, err := json.Marshal(event)
			if err != nil {
				continue // skip malformed event
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func ok(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    data,
	})
}

func accepted(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Data:    data,
	})
}

func badRequest(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusBadRequest, apiResponse{
		Success: false,
		Data:    data,
	})
}

func notFound(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusNotFound, apiResponse{
		Success: false,
		Data:    data,
	})
}

func internalError(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusInternalServerError, apiResponse{
		Success: false,
		Data:    data,
	})
}
