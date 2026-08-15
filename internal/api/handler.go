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
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
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
		badRequest(w, "invalid request body", nil)
		return
	}

	githubURL := strings.TrimSpace(req.GithubURL)
	_, _, _, err = service.ParseGitHubRepoURL(githubURL)
	if err != nil {
		badRequest(w, "invalid request body", nil)
		return
	}

	deployment, err := h.service.Deploy(r.Context(), githubURL)
	if err != nil {
		internalError(w, "failed to create deployment", nil)
		return
	}

	accepted(w, "deployment created successfully", deployment)
}

func (h *deploymentHandler) List(w http.ResponseWriter, r *http.Request) {
	deployments, err := h.db.ListDeployments(r.Context())
	if err != nil {
		internalError(w, "failed to fetch deployments", nil)
		return
	}

	ok(w, "deployments fetched successfully", deployments)
}

func (h *deploymentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deployment, err := h.db.GetDeploymentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found", nil)
			return
		}

		internalError(w, "failed to fetch deployment", nil)
		return
	}

	ok(w, "deployment fetched successfully", deployment)

}

func (h *deploymentHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.service.Stop(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found", nil)
			return
		}

		internalError(w, "failed to stop deployment", nil)
		return
	}

	ok(w, "deployment stopped successfully", nil)
}

func (h *deploymentHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, err := h.db.GetDeploymentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w, "deployment not found", nil)
			return
		}

		internalError(w, "failed to fetch deployment", nil)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		internalError(w, "streaming is not supported", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	stream := h.streams.Subscribe(id)
	defer h.streams.Unsubscribe(id, stream)

	cursor := r.Header.Get("Last-Event-ID")

	drain := func() {
		entries, err := h.db.GetLogsAfterID(r.Context(), id, cursor)
		if err != nil {
			return
		}

		for _, l := range entries {
			payload, err := json.Marshal(l)
			if err != nil {
				return
			}

			fmt.Fprintf(w, "id: %s\nevent: log\ndata: %s\n\n", l.ID, payload)
			cursor = l.ID
		}

		if len(entries) > 0 {
			flusher.Flush()
		}
	}

	drain()

	for {
		select {
		case <-r.Context().Done():
			return

		case event, open := <-stream:
			if !open {
				return
			}

			if event.ID <= cursor {
				continue
			}

			payload, err := json.Marshal(event)
			if err != nil {
				continue // skip malformed event
			}

			fmt.Fprintf(w, "id: %s\nevent: log\ndata: %s\n\n", event.ID, payload)
			cursor = event.ID
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func ok(w http.ResponseWriter, message string, data any) {
	writeJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func accepted(w http.ResponseWriter, message string, data any) {
	writeJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func badRequest(w http.ResponseWriter, message string, data any) {
	writeJSON(w, http.StatusBadRequest, apiResponse{
		Success: false,
		Message: message,
		Data:    data,
	})
}

func notFound(w http.ResponseWriter, message string, data any) {
	writeJSON(w, http.StatusNotFound, apiResponse{
		Success: false,
		Message: message,
		Data:    data,
	})
}

func internalError(w http.ResponseWriter, message string, data any) {
	writeJSON(w, http.StatusInternalServerError, apiResponse{
		Success: false,
		Message: message,
		Data:    data,
	})
}
