package docker

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const ManagedLabel = "managed-by=mpaas"

type Client struct {
	cli *client.Client
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

func (c *Client) RunContainer(ctx context.Context, containerPort, containerName, imageName, deployID, networkName string) (containerID, hostPort string, err error) {
	if containerPort == "" {
		containerPort = "8080"
	}

	port, err := network.ParsePort(containerPort + "/tcp")
	if err != nil {
		return "", "", fmt.Errorf("invalid container port %q: %w", containerPort, err)
	}

	labels := labelMap()
	labels["deploy-id"] = deployID

	cfg := &container.Config{
		Image:        imageName,
		Env:          []string{"PORT=" + containerPort},
		Labels:       labels,
		ExposedPorts: network.PortSet{port: {}},
	}

	hostCfg := &container.HostConfig{
		PortBindings: network.PortMap{
			port: {
				{
					HostIP:   netip.MustParseAddr("0.0.0.0"),
					HostPort: "",
				},
			},
		},
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{networkName: {}},
	}

	created, err := c.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           cfg,
		HostConfig:       hostCfg,
		NetworkingConfig: netCfg,
		Name:             containerName,
	})
	if err != nil {
		return "", "", fmt.Errorf("container create: %w", err)
	}

	_, err = c.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if err != nil {
		_ = c.Remove(ctx, created.ID)
		return "", "", fmt.Errorf("container start: %w", err)
	}

	hostPort, err = c.getHostPort(ctx, created.ID, port)
	if err != nil {
		_ = c.Remove(ctx, created.ID)
		return "", "", fmt.Errorf("get host port: %w", err)
	}

	return created.ID, hostPort, err
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
