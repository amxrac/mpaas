package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const serverID = "srv0"

type Client struct {
	adminURL   string
	httpClient *http.Client
}

type RouteOpts struct {
	DeploymentID string
	Host         string
	Upstream     string
}

type Route struct {
	ID       string   `json:"@id"`
	Match    []match  `json:"match"`
	Handle   []handle `json:"handle"`
	Terminal bool     `json:"terminal"`
}

type match struct {
	Host []string `json:"host"`
}

type handle struct {
	Handler   string      `json:"handler"`
	Upstreams []upstreams `json:"upstreams"`
}

type upstreams struct {
	Dial string `json:"dial"`
}

func NewClient(adminURL string) *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
	}

	return &Client{
		adminURL:   adminURL,
		httpClient: &http.Client{Transport: transport, Timeout: 15 * time.Second},
	}
}

func (c *Client) AddRoute(ctx context.Context, route RouteOpts) error {
	routeID := "deploy-" + route.DeploymentID
	r := Route{
		ID:    routeID,
		Match: []match{{Host: []string{route.Host}}},
		Handle: []handle{{
			Handler:   "reverse_proxy",
			Upstreams: []upstreams{{Dial: route.Upstream}},
		}},
		Terminal: true,
	}

	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal caddy route: %w", err)
	}

	url, err := c.constructURL("/config/apps/http/servers/" + serverID + "/routes/0")
	if err != nil {
		return fmt.Errorf("caddy add route: invalid url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("caddy add route request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("caddy add route for %q: %w", route.Host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		r, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy add route: status %d: %s", resp.StatusCode, string(r))
	}
	return nil
}

func (c *Client) RemoveRoute(ctx context.Context, deploymentID string) error {
	routeID := "deploy-" + deploymentID
	url, err := c.constructURL("/id/" + routeID)
	if err != nil {
		return fmt.Errorf("caddy remove route: invalid url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("caddy remove route request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("caddy delete route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	r, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("caddy remove route: status %d: %s", resp.StatusCode, string(r))
}

func (c *Client) constructURL(path string) (string, error) {
	return url.JoinPath(c.adminURL, path)
}

func (c *Client) EnsureCaddyReady(ctx context.Context, timeout time.Duration) error {
	var err error
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err = c.RemoveRoute(ctx, "_sample_id_")
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("caddy api not ready after %s: %w", timeout, err)
}
