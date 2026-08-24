package caddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAddRoute(t *testing.T) {
	var (
		gotMethod      string
		gotPath        string
		gotContentType string
		gotRoute       Route
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}

		err = json.Unmarshal(body, &gotRoute)
		if err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	err := c.AddRoute(context.Background(), RouteOpts{
		DeploymentID: "abc123",
		Host:         "abc123.localhost",
		Upstream:     "deploy-abc123:3000",
	})
	if err != nil {
		t.Fatalf("AddRoute() error = %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodPut)
	}

	wantPath := "/config/apps/http/servers/" + serverID + "/routes/0"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	if gotRoute.ID != "deploy-abc123" {
		t.Errorf("route ID = %q, want deploy-abc123", gotRoute.ID)
	}

	if len(gotRoute.Match) != 1 ||
		len(gotRoute.Match[0].Host) != 1 ||
		gotRoute.Match[0].Host[0] != "abc123.localhost" {
		t.Errorf("unexpected route match: %+v", gotRoute.Match)
	}

	if len(gotRoute.Handle) != 1 {
		t.Fatalf("handle count = %d, want 1", len(gotRoute.Handle))
	}

	handle := gotRoute.Handle[0]

	if handle.Handler != "reverse_proxy" {
		t.Errorf("handler = %q, want reverse_proxy", handle.Handler)
	}

	if len(handle.Upstreams) != 1 {
		t.Fatalf("upstream count = %d, want 1", len(handle.Upstreams))
	}

	if handle.Upstreams[0].Dial != "deploy-abc123:3000" {
		t.Errorf(
			"upstream = %q, want deploy-abc123:3000",
			handle.Upstreams[0].Dial,
		)
	}

	if !gotRoute.Terminal {
		t.Error("route Terminal = false, want true")
	}
}

func TestAddRouteAcceptedStatus(t *testing.T) {
	statuses := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusAccepted,
	}

	for _, status := range statuses {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := NewClient(srv.URL)

			err := c.AddRoute(context.Background(), RouteOpts{
				DeploymentID: "abc123",
				Host:         "abc123.localhost",
				Upstream:     "deploy-abc123:3000",
			})
			if err != nil {
				t.Fatalf("AddRoute() status %d: %v", status, err)
			}
		})
	}
}

func TestAddRouteErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid route", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	err := c.AddRoute(context.Background(), RouteOpts{
		DeploymentID: "abc123",
		Host:         "abc123.localhost",
		Upstream:     "deploy-abc123:3000",
	})
	if err == nil {
		t.Fatal("AddRoute() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want status 400", err)
	}

	if !strings.Contains(err.Error(), "invalid route") {
		t.Errorf("error = %q, want response body", err)
	}
}

func TestRemoveRoute(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want %q", r.Method, http.MethodDelete)
		}

		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	err := c.RemoveRoute(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("RemoveRoute() error = %v", err)
	}

	wantPath := "/id/deploy-abc123"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestRemoveRouteNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	err := c.RemoveRoute(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("RemoveRoute() returned error for 404: %v", err)
	}
}

func TestRemoveRouteErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "route failure", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)

	err := c.RemoveRoute(context.Background(), "abc123")
	if err == nil {
		t.Fatal("RemoveRoute() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error = %q, want status 500", err)
	}

	if !strings.Contains(err.Error(), "route failure") {
		t.Errorf("error = %q, want response body", err)
	}
}

func TestConstructURL(t *testing.T) {
	c := NewClient("http://localhost:2019/")

	got, err := c.constructURL("/id/deploy-abc123")
	if err != nil {
		t.Fatalf("constructURL() error = %v", err)
	}

	want := "http://localhost:2019/id/deploy-abc123"
	if got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
}
