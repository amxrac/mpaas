package docker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type fakeDockerClient struct {
	containerCreate  func(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	containerStart   func(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	containerStop    func(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
	containerRemove  func(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	containerList    func(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	containerLogs    func(context.Context, string, client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	containerInspect func(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	networkCreate    func(context.Context, string, client.NetworkCreateOptions) (client.NetworkCreateResult, error)
	ping             func(context.Context, client.PingOptions) (client.PingResult, error)
	close            func() error
}

func (f *fakeDockerClient) ContainerCreate(
	ctx context.Context,
	options client.ContainerCreateOptions,
) (client.ContainerCreateResult, error) {
	return f.containerCreate(ctx, options)
}

func (f *fakeDockerClient) ContainerStart(
	ctx context.Context,
	id string,
	options client.ContainerStartOptions,
) (client.ContainerStartResult, error) {
	return f.containerStart(ctx, id, options)
}

func (f *fakeDockerClient) ContainerStop(
	ctx context.Context,
	id string,
	options client.ContainerStopOptions,
) (client.ContainerStopResult, error) {
	return f.containerStop(ctx, id, options)
}

func (f *fakeDockerClient) ContainerRemove(
	ctx context.Context,
	id string,
	options client.ContainerRemoveOptions,
) (client.ContainerRemoveResult, error) {
	return f.containerRemove(ctx, id, options)
}

func (f *fakeDockerClient) ContainerList(
	ctx context.Context,
	options client.ContainerListOptions,
) (client.ContainerListResult, error) {
	return f.containerList(ctx, options)
}

func (f *fakeDockerClient) ContainerLogs(
	ctx context.Context,
	id string,
	options client.ContainerLogsOptions,
) (client.ContainerLogsResult, error) {
	return f.containerLogs(ctx, id, options)
}

func (f *fakeDockerClient) ContainerInspect(
	ctx context.Context,
	id string,
	options client.ContainerInspectOptions,
) (client.ContainerInspectResult, error) {
	return f.containerInspect(ctx, id, options)
}

func (f *fakeDockerClient) NetworkCreate(
	ctx context.Context,
	name string,
	options client.NetworkCreateOptions,
) (client.NetworkCreateResult, error) {
	return f.networkCreate(ctx, name, options)
}

func (f *fakeDockerClient) Ping(
	ctx context.Context,
	options client.PingOptions,
) (client.PingResult, error) {
	return f.ping(ctx, options)
}

func (f *fakeDockerClient) Close() error {
	return f.close()
}

func TestPing(t *testing.T) {
	wantErr := errors.New("docker unavailable")

	fake := &fakeDockerClient{
		ping: func(context.Context, client.PingOptions) (client.PingResult, error) {
			return client.PingResult{}, wantErr
		},
	}

	c := &Client{cli: fake}

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() expected error, got nil")
	}

	if !strings.Contains(err.Error(), "docker not running or unavailable") {
		t.Errorf("error = %q, want docker unavailable message", err)
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original error: %v", err)
	}
}

func TestPingSuccess(t *testing.T) {
	fake := &fakeDockerClient{
		ping: func(context.Context, client.PingOptions) (client.PingResult, error) {
			return client.PingResult{}, nil
		},
	}

	c := &Client{cli: fake}

	err := c.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func TestEnsureNetwork(t *testing.T) {
	var (
		gotName string
		gotOpts client.NetworkCreateOptions
	)

	fake := &fakeDockerClient{
		networkCreate: func(
			_ context.Context,
			name string,
			options client.NetworkCreateOptions,
		) (client.NetworkCreateResult, error) {
			gotName = name
			gotOpts = options
			return client.NetworkCreateResult{}, nil
		},
	}

	c := &Client{cli: fake}

	err := c.EnsureNetwork(context.Background(), "mpaas")
	if err != nil {
		t.Fatalf("EnsureNetwork() error = %v", err)
	}

	if gotName != "mpaas" {
		t.Errorf("network name = %q, want mpaas", gotName)
	}

	if gotOpts.Labels["managed-by"] != "mpaas" {
		t.Errorf("managed-by label = %q, want mpaas", gotOpts.Labels["managed-by"])
	}
}

func TestEnsureNetworkError(t *testing.T) {
	wantErr := errors.New("network failure")

	fake := &fakeDockerClient{
		networkCreate: func(
			context.Context,
			string,
			client.NetworkCreateOptions,
		) (client.NetworkCreateResult, error) {
			return client.NetworkCreateResult{}, wantErr
		},
	}

	c := &Client{cli: fake}

	err := c.EnsureNetwork(context.Background(), "mpaas")
	if err == nil {
		t.Fatal("EnsureNetwork() expected error, got nil")
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original error: %v", err)
	}
}

func TestRunContainer(t *testing.T) {
	var (
		gotCreate  client.ContainerCreateOptions
		gotStartID string
	)

	fake := &fakeDockerClient{
		containerCreate: func(
			_ context.Context,
			options client.ContainerCreateOptions,
		) (client.ContainerCreateResult, error) {
			gotCreate = options

			return client.ContainerCreateResult{
				ID: "container-123",
			}, nil
		},
		containerStart: func(
			_ context.Context,
			id string,
			_ client.ContainerStartOptions,
		) (client.ContainerStartResult, error) {
			gotStartID = id
			return client.ContainerStartResult{}, nil
		},
	}

	c := &Client{cli: fake}

	id, port, err := c.RunContainer(context.Background(), RunContainerOpts{
		ContainerPort: "3000",
		ContainerName: "test-container",
		ImageName:     "test/image",
		DeployID:      "deploy-123",
		NetworkName:   "mpaas",
	})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}

	if id != "container-123" {
		t.Errorf("container ID = %q, want container-123", id)
	}

	if port != 3000 {
		t.Errorf("port = %d, want 3000", port)
	}

	if gotStartID != "container-123" {
		t.Errorf("start ID = %q, want container-123", gotStartID)
	}

	if gotCreate.Name != "test-container" {
		t.Errorf("container name = %q, want test-container", gotCreate.Name)
	}

	if gotCreate.Config.Image != "test/image" {
		t.Errorf("image = %q, want test/image", gotCreate.Config.Image)
	}

	if gotCreate.Config.Env[0] != "PORT=3000" {
		t.Errorf("env = %v, want PORT=3000", gotCreate.Config.Env)
	}

	if gotCreate.Config.Labels["managed-by"] != "mpaas" {
		t.Errorf("managed-by label = %q, want mpaas", gotCreate.Config.Labels["managed-by"])
	}

	if gotCreate.Config.Labels["deploy-id"] != "deploy-123" {
		t.Errorf("deploy-id label = %q, want deploy-123", gotCreate.Config.Labels["deploy-id"])
	}

	if gotCreate.HostConfig.NetworkMode != "mpaas" {
		t.Errorf("network = %q, want mpaas", gotCreate.HostConfig.NetworkMode)
	}

	parsedPort, err := network.ParsePort("3000/tcp")
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	_, ok := gotCreate.Config.ExposedPorts[parsedPort]
	if !ok {
		t.Errorf("port 3000/tcp was not exposed")
	}
}

func TestRunContainerDefaultPort(t *testing.T) {
	var gotCreate client.ContainerCreateOptions

	fake := &fakeDockerClient{
		containerCreate: func(
			_ context.Context,
			options client.ContainerCreateOptions,
		) (client.ContainerCreateResult, error) {
			gotCreate = options
			return client.ContainerCreateResult{ID: "container-123"}, nil
		},
		containerStart: func(
			context.Context,
			string,
			client.ContainerStartOptions,
		) (client.ContainerStartResult, error) {
			return client.ContainerStartResult{}, nil
		},
	}

	c := &Client{cli: fake}

	_, port, err := c.RunContainer(context.Background(), RunContainerOpts{})
	if err != nil {
		t.Fatalf("RunContainer() error = %v", err)
	}

	if port != 8080 {
		t.Errorf("port = %d, want 8080", port)
	}

	if gotCreate.Config.Env[0] != "PORT=8080" {
		t.Errorf("env = %v, want PORT=8080", gotCreate.Config.Env)
	}
}

func TestRunContainerInvalidPort(t *testing.T) {
	fake := &fakeDockerClient{
		containerCreate: func(
			context.Context,
			client.ContainerCreateOptions,
		) (client.ContainerCreateResult, error) {
			t.Fatal("ContainerCreate should not be called")
			return client.ContainerCreateResult{}, nil
		},
	}

	c := &Client{cli: fake}

	_, _, err := c.RunContainer(context.Background(), RunContainerOpts{
		ContainerPort: "invalid",
	})
	if err == nil {
		t.Fatal("RunContainer() expected error, got nil")
	}

	if !strings.Contains(err.Error(), `invalid container port "invalid"`) {
		t.Errorf("error = %q", err)
	}
}

func TestRunContainerCreateError(t *testing.T) {
	wantErr := errors.New("create failed")

	fake := &fakeDockerClient{
		containerCreate: func(
			context.Context,
			client.ContainerCreateOptions,
		) (client.ContainerCreateResult, error) {
			return client.ContainerCreateResult{}, wantErr
		},
	}

	c := &Client{cli: fake}

	_, port, err := c.RunContainer(context.Background(), RunContainerOpts{
		ContainerPort: "3000",
	})
	if err == nil {
		t.Fatal("RunContainer() expected error, got nil")
	}

	if port != 3000 {
		t.Errorf("port = %d, want 3000", port)
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original error: %v", err)
	}
}

func TestRunContainerStartErrorRemovesContainer(t *testing.T) {
	startErr := errors.New("start failed")

	var removedID string

	fake := &fakeDockerClient{
		containerCreate: func(
			context.Context,
			client.ContainerCreateOptions,
		) (client.ContainerCreateResult, error) {
			return client.ContainerCreateResult{ID: "container-123"}, nil
		},
		containerStart: func(
			context.Context,
			string,
			client.ContainerStartOptions,
		) (client.ContainerStartResult, error) {
			return client.ContainerStartResult{}, startErr
		},
		containerRemove: func(
			_ context.Context,
			id string,
			options client.ContainerRemoveOptions,
		) (client.ContainerRemoveResult, error) {
			removedID = id

			if !options.Force {
				t.Error("Remove Force = false, want true")
			}

			if !options.RemoveVolumes {
				t.Error("RemoveVolumes = false, want true")
			}

			return client.ContainerRemoveResult{}, nil
		},
	}

	c := &Client{cli: fake}

	_, _, err := c.RunContainer(context.Background(), RunContainerOpts{})
	if err == nil {
		t.Fatal("RunContainer() expected error, got nil")
	}

	if !errors.Is(err, startErr) {
		t.Errorf("error does not wrap original error: %v", err)
	}

	if removedID != "container-123" {
		t.Errorf("removed ID = %q, want container-123", removedID)
	}
}

func TestStop(t *testing.T) {
	var gotTimeout int

	fake := &fakeDockerClient{
		containerStop: func(
			_ context.Context,
			id string,
			options client.ContainerStopOptions,
		) (client.ContainerStopResult, error) {
			if id != "container-123" {
				t.Errorf("container ID = %q, want container-123", id)
			}

			if options.Timeout == nil {
				t.Fatal("timeout is nil")
			}

			gotTimeout = *options.Timeout
			return client.ContainerStopResult{}, nil
		},
	}

	c := &Client{cli: fake}

	err := c.Stop(context.Background(), "container-123", 10)
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if gotTimeout != 10 {
		t.Errorf("timeout = %d, want 10", gotTimeout)
	}
}

func TestRemove(t *testing.T) {
	var gotOpts client.ContainerRemoveOptions

	fake := &fakeDockerClient{
		containerRemove: func(
			_ context.Context,
			_ string,
			options client.ContainerRemoveOptions,
		) (client.ContainerRemoveResult, error) {
			gotOpts = options
			return client.ContainerRemoveResult{}, nil
		},
	}

	c := &Client{cli: fake}

	err := c.Remove(context.Background(), "container-123")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if !gotOpts.Force {
		t.Error("Force = false, want true")
	}

	if !gotOpts.RemoveVolumes {
		t.Error("RemoveVolumes = false, want true")
	}
}

func TestStopThenRemove(t *testing.T) {
	var (
		stopCalled   bool
		removeCalled bool
	)

	fake := &fakeDockerClient{
		containerStop: func(
			_ context.Context,
			id string,
			options client.ContainerStopOptions,
		) (client.ContainerStopResult, error) {
			stopCalled = true

			if id != "container-123" {
				t.Errorf("stop ID = %q, want container-123", id)
			}

			if options.Timeout == nil || *options.Timeout != 10 {
				t.Errorf("stop timeout = %v, want 10", options.Timeout)
			}

			return client.ContainerStopResult{}, nil
		},
		containerRemove: func(
			_ context.Context,
			id string,
			_ client.ContainerRemoveOptions,
		) (client.ContainerRemoveResult, error) {
			removeCalled = true

			if id != "container-123" {
				t.Errorf("remove ID = %q, want container-123", id)
			}

			return client.ContainerRemoveResult{}, nil
		},
	}

	c := &Client{cli: fake}

	err := c.StopThenRemove(context.Background(), "container-123")
	if err != nil {
		t.Fatalf("StopThenRemove() error = %v", err)
	}

	if !stopCalled {
		t.Error("ContainerStop was not called")
	}

	if !removeCalled {
		t.Error("ContainerRemove was not called")
	}
}

func TestListManaged(t *testing.T) {
	var gotOpts client.ContainerListOptions

	want := []container.Summary{
		{ID: "container-1"},
		{ID: "container-2"},
	}

	fake := &fakeDockerClient{
		containerList: func(
			_ context.Context,
			options client.ContainerListOptions,
		) (client.ContainerListResult, error) {
			gotOpts = options
			return client.ContainerListResult{Items: want}, nil
		},
	}

	c := &Client{cli: fake}

	got, err := c.ListManaged(context.Background())
	if err != nil {
		t.Fatalf("ListManaged() error = %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("len(containers) = %d, want %d", len(got), len(want))
	}

	if !gotOpts.All {
		t.Error("ContainerList All = false, want true")
	}

	labels, ok := gotOpts.Filters["label"]
	if !ok || !labels[ManagedLabel] {
		t.Errorf("label filter missing or not set to %q", ManagedLabel)
	}
}

func TestListManagedError(t *testing.T) {
	wantErr := errors.New("list failed")

	fake := &fakeDockerClient{
		containerList: func(
			context.Context,
			client.ContainerListOptions,
		) (client.ContainerListResult, error) {
			return client.ContainerListResult{}, wantErr
		},
	}

	c := &Client{cli: fake}

	_, err := c.ListManaged(context.Background())
	if err == nil {
		t.Fatal("ListManaged() expected error, got nil")
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original error: %v", err)
	}
}

func TestLogs(t *testing.T) {
	var gotOpts client.ContainerLogsOptions

	body := io.NopCloser(strings.NewReader("logs"))

	fake := &fakeDockerClient{
		containerLogs: func(
			_ context.Context,
			id string,
			options client.ContainerLogsOptions,
		) (client.ContainerLogsResult, error) {
			if id != "container-123" {
				t.Errorf("container ID = %q, want container-123", id)
			}

			gotOpts = options
			return body, nil
		},
	}

	c := &Client{cli: fake}

	got, err := c.Logs(context.Background(), "container-123", true)
	if err != nil {
		t.Fatalf("Logs() error = %v", err)
	}

	if got != body {
		t.Error("Logs() returned a different reader")
	}

	if !gotOpts.ShowStdout {
		t.Error("ShowStdout = false, want true")
	}

	if !gotOpts.ShowStderr {
		t.Error("ShowStderr = false, want true")
	}

	if !gotOpts.Timestamps {
		t.Error("Timestamps = false, want true")
	}

	if !gotOpts.Follow {
		t.Error("Follow = false, want true")
	}
}

func TestClose(t *testing.T) {
	called := false

	fake := &fakeDockerClient{
		close: func() error {
			called = true
			return nil
		},
	}

	c := &Client{cli: fake}

	err := c.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !called {
		t.Error("Close() did not call underlying client")
	}
}

func TestLabelMap(t *testing.T) {
	got := labelMap()

	if got["managed-by"] != "mpaas" {
		t.Errorf("managed-by = %q, want mpaas", got["managed-by"])
	}

	if len(got) != 1 {
		t.Errorf("len(labels) = %d, want 1", len(got))
	}
}
