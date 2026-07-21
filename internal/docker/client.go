package docker

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	caddyContainerName = "caddy"
	caddyImage         = "caddy:2.11.4-alpine"
	ManagedLabel       = "managed-by=mpaas"
)

type Client struct {
	cli *client.Client
}

type RunContainerOpts struct {
	ContainerPort string
	ContainerName string
	ImageName     string
	DeployID      string
	NetworkName   string
}

func NewClient(ctx context.Context) (*Client, error) {
	host, err := resolveDockerHost(ctx)
	if err != nil {
		return nil, err
	}

	opts := []client.Opt{}

	if host != "" {
		opts = append(opts, client.WithHost(host))
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		return fmt.Errorf("docker not running or unavailable: %w", err)
	}
	return nil
}

func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	_, err := c.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{
		Labels: labelMap(),
	})
	if err == nil || cerrdefs.IsConflict(err) {
		return nil
	}
	return fmt.Errorf("create network %q: %w", name, err)
}

func (c *Client) EnsureCaddy(ctx context.Context, caddyfilePath, networkName string) error {
	info, err := c.cli.ContainerInspect(ctx, caddyContainerName, client.ContainerInspectOptions{})

	if err == nil {
		if info.Container.State.Running {
			return nil
		}
		_, err = c.cli.ContainerStart(ctx, caddyContainerName, client.ContainerStartOptions{})
		if err != nil {
			_ = c.Remove(ctx, caddyContainerName)
			return fmt.Errorf("start existing caddy container: %w", err)
		}
	}

	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspect caddy container: %w", err)
	}

	absCaddyfile, err := filepath.Abs(caddyfilePath)
	if err != nil {
		return fmt.Errorf("resolve caddyfile path: %w", err)
	}

	fileInfo, err := os.Stat(absCaddyfile)
	if err != nil {
		return fmt.Errorf("caddyfile not found at %s: %w", absCaddyfile, err)
	}
	if fileInfo.IsDir() {
		return fmt.Errorf("expected a file at %s, found a directory", absCaddyfile)
	}

	r, err := c.cli.ImagePull(ctx, caddyImage, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull caddy image %q: %w", caddyImage, err)
	}
	defer r.Close()

	_, err = io.Copy(io.Discard, r)
	if err != nil {
		return fmt.Errorf("pull caddy image %q: %w", caddyImage, err)
	}

	labels := labelMap()

	cfg := &container.Config{
		Image:  caddyImage,
		Labels: labels,
	}

	hostCfg := &container.HostConfig{
		Binds: []string{absCaddyfile + ":/etc/caddy/Caddyfile:ro"},
		PortBindings: network.PortMap{
			network.MustParsePort("80/tcp"):   {{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: "80"}},
			network.MustParsePort("443/tcp"):  {{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: "443"}},
			network.MustParsePort("2019/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "2019"}},
		},
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}},
	}

	created, err := c.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           cfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		Name:             caddyContainerName,
	})

	if err != nil {
		return fmt.Errorf("create caddy container: %w", err)
	}

	_, err = c.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if err != nil {
		_ = c.Remove(ctx, created.ID)
		return fmt.Errorf("start caddy container: %w", err)
	}

	return nil
}

func (c *Client) RunContainer(ctx context.Context, opts RunContainerOpts) (containerID string, err error) {
	if opts.ContainerPort == "" {
		opts.ContainerPort = "8080"
	}

	port, err := network.ParsePort(opts.ContainerPort + "/tcp")
	if err != nil {
		return "", fmt.Errorf("invalid container port %q: %w", opts.ContainerPort, err)
	}

	labels := labelMap()
	labels["deploy-id"] = opts.DeployID

	cfg := &container.Config{
		Image:        opts.ImageName,
		Env:          []string{"PORT=" + opts.ContainerPort},
		Labels:       labels,
		ExposedPorts: network.PortSet{port: {}},
	}

	hostCfg := &container.HostConfig{
		NetworkMode: container.NetworkMode(opts.NetworkName),
	}

	created, err := c.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: hostCfg,
		Name:       opts.ContainerName,
	})
	if err != nil {
		return "", fmt.Errorf("container create: %w", err)
	}

	_, err = c.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if err != nil {
		_ = c.Remove(ctx, created.ID)
		return "", fmt.Errorf("container start: %w", err)
	}

	return created.ID, err
}

func (c *Client) Stop(ctx context.Context, containerID string, timeoutSecs int) error {
	_, err := c.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeoutSecs})
	if err != nil {
		return fmt.Errorf("container stop: %w", err)
	}
	return nil
}

func (c *Client) Remove(ctx context.Context, containerID string) error {
	_, err := c.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})

	if err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("container remove: %w", err)
	}
	return nil
}

func (c *Client) StopThenRemove(ctx context.Context, id string) error {
	_ = c.Stop(ctx, id, 10)
	return c.Remove(ctx, id)
}

func (c *Client) Close() error {
	return c.cli.Close()
}

func (c *Client) ListManaged(ctx context.Context) ([]container.Summary, error) {
	f := client.Filters{}.Add("label", ManagedLabel)
	list, err := c.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}
	return list.Items, nil
}

func (c *Client) Logs(ctx context.Context, containerID string, follow bool) (io.ReadCloser, error) {
	return c.cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	})
}

func (c *Client) CaddyhttpHealthCheck(ctx context.Context, targetHost string, targetPort int) error {
	resp, err := c.cli.ExecCreate(ctx, caddyContainerName, client.ExecCreateOptions{
		Cmd: []string{"wget", "-q", "-T", "2", "-O", "/dev/null",
			fmt.Sprintf("http://%s:%d/", targetHost, targetPort)},
	})
	if err != nil {
		return fmt.Errorf("create healthcheck exec: %w", err)
	}

	_, err = c.cli.ExecStart(ctx, resp.ID, client.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("start healthcheck exec: %w", err)
	}

	for {
		inspect, err := c.cli.ExecInspect(ctx, resp.ID, client.ExecInspectOptions{})
		if err != nil {
			return fmt.Errorf("inspect healthcheck exec: %w", err)
		}
		if !inspect.Running {
			if inspect.ExitCode != 0 {
				return fmt.Errorf("healthcheck failed: wget exited %d", inspect.ExitCode)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}

}

func (c *Client) getHostPort(ctx context.Context, containerID string, port network.Port) (string, error) {
	info, err := c.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("container inspect: %w", err)
	}

	bindings := info.Container.NetworkSettings.Ports[port]
	if len(bindings) == 0 {
		return "", fmt.Errorf("no host binding for %s", port)
	}

	return bindings[0].HostPort, nil
}

func resolveDockerHost(ctx context.Context) (string, error) {
	h := os.Getenv("DOCKER_HOST")
	if h != "" {
		return h, nil
	}

	out, err := exec.CommandContext(
		ctx,
		"docker",
		"context",
		"inspect",
		"-f",
		"{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return "", fmt.Errorf("inspect docker context: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

func labelMap() map[string]string {
	k, v, _ := strings.Cut(ManagedLabel, "=")
	return map[string]string{k: v}
}
