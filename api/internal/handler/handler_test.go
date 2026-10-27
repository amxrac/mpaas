package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/amxrac/mpaas/api/internal/service"
	"github.com/amxrac/mpaas/api/internal/stream"
)

// these tests only cover paths that fail before the deploy pipeline starts.
// adding a valid create request here would start the pipeline goroutine
// whose nil-client
// panic crashes the whole test binary.
func newTestRouter(t *testing.T) (http.Handler, *db.DB) {
	t.Helper()

	database, err := db.ConnectDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := database.Migrate(&models.Deployment{}, &models.LogEntry{}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	hub := stream.NewHub()
	svc := service.NewService(database, hub, nil, nil)
	dh := NewDeploymentHandler(svc, database, hub)
	return Setup(NewHandler(dh, database, nil)), database
}

// sends a request through the router and decodes the API envelope.
func doJSON(t *testing.T, router http.Handler, method, path, body string) (int, apiResponse) {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp apiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, resp
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	const validURL = `"github_url":"https://github.com/owner/repo"`

	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"malformed json", `{`, "invalid request body"},
		{"non-string env value", `{` + validURL + `,"env":{"K":1}}`, "invalid request body"},
		{"missing url", `{}`, "invalid github url"},
		{"non-github url", `{"github_url":"https://gitlab.com/owner/repo"}`, "invalid github url"},
		{"port too high", `{` + validURL + `,"port":70000}`, service.ErrInvalidPort.Error()},
		{"negative port", `{` + validURL + `,"port":-1}`, service.ErrInvalidPort.Error()},
		{"env key starts with digit", `{` + validURL + `,"env":{"1BAD":"x"}}`, service.ErrInvalidEnvVar.Error()},
		{"env key with dash", `{` + validURL + `,"env":{"MY-KEY":"x"}}`, service.ErrInvalidEnvVar.Error()},
		{"env key PORT", `{` + validURL + `,"env":{"PORT":"1"}}`, service.ErrInvalidEnvVar.Error()},
		{"root dir escapes repo", `{` + validURL + `,"root_dir":"../x"}`, service.ErrInvalidRootDir.Error()},
		{"root dir escapes after clean", `{` + validURL + `,"root_dir":"app/../../x"}`, service.ErrInvalidRootDir.Error()},
		{"absolute root dir", `{` + validURL + `,"root_dir":"/app"}`, service.ErrInvalidRootDir.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, database := newTestRouter(t)

			code, resp := doJSON(t, router, http.MethodPost, "/api/deployments", tt.body)

			if code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", code, http.StatusBadRequest)
			}
			if resp.Success {
				t.Error("success = true, want false")
			}
			if !strings.Contains(resp.Message, tt.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", resp.Message, tt.wantMsg)
			}

			deployments, err := database.ListDeployments(t.Context())
			if err != nil {
				t.Fatalf("list deployments: %v", err)
			}
			if len(deployments) != 0 {
				t.Errorf("deployments = %d, want 0", len(deployments))
			}
		})
	}
}

func TestUnknownDeploymentReturnsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"get", http.MethodGet, "/api/deployments/does-not-exist"},
		{"stop", http.MethodPost, "/api/deployments/does-not-exist/stop"},
		{"delete", http.MethodDelete, "/api/deployments/does-not-exist"},
		{"stream logs", http.MethodGet, "/api/deployments/does-not-exist/logs/stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newTestRouter(t)

			code, resp := doJSON(t, router, tt.method, tt.path, "")

			if code != http.StatusNotFound {
				t.Errorf("status = %d, want %d", code, http.StatusNotFound)
			}
			if resp.Success {
				t.Error("success = true, want false")
			}
		})
	}
}

func TestListEmpty(t *testing.T) {
	router, _ := newTestRouter(t)

	code, resp := doJSON(t, router, http.MethodGet, "/api/deployments", "")

	if code != http.StatusOK {
		t.Errorf("status = %d, want %d", code, http.StatusOK)
	}
	if !resp.Success {
		t.Error("success = false, want true")
	}
}

func TestCORSPreflight(t *testing.T) {
	router, _ := newTestRouter(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/deployments", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}
