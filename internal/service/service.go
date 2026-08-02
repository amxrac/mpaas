package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amxrac/mpaas/internal/caddy"
	"github.com/amxrac/mpaas/internal/db"
	"github.com/amxrac/mpaas/internal/docker"
	"github.com/amxrac/mpaas/internal/models"
	"github.com/amxrac/mpaas/internal/stream"
)

var errInvalidGitHubRepoURL = errors.New("invalid github repo url")
var validRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Service struct {
	db           *db.DB
	stream       *stream.Hub
	dockerClient *docker.Client
	caddyClient  *caddy.Client
	runsMu       sync.Mutex
	runs         map[string]*runHandle
}

func NewService(
	db *db.DB,
	stream *stream.Hub,

	dockerClient *docker.Client,
	caddyClient *caddy.Client,
) *Service {
	return &Service{
		db:           db,
		stream:       stream,
		dockerClient: dockerClient,
		caddyClient:  caddyClient,
		runs:         make(map[string]*runHandle),
	}
}

// Deploy creates a new deployment record,
// sets up a cancellable background context,
// and starts run on a single goroutine.
func (s *Service) Deploy(ctx context.Context, githubURL string) (*models.Deployment, error) {
	deployment := &models.Deployment{
		GithubURL: githubURL,
		Status:    models.StatusPending,
	}

	err := s.db.InsertDeployment(ctx, deployment)
	if err != nil {
		s.emitLog(ctx, "insert deployment error", deployment.ID)
		return nil, fmt.Errorf("insert deployment: %w", err)
	}

	s.emitLog(ctx, "deployment created", deployment.ID)

	asyncCtx, cancel := context.WithCancel(context.Background())
	handle := &runHandle{cancel: cancel, done: make(chan struct{})}

	s.runsMu.Lock()
	s.runs[deployment.ID] = handle
	s.runsMu.Unlock()

	deploymentCopy := *deployment
	go s.run(asyncCtx, &deploymentCopy, handle)

	return deployment, nil
}

func (s *Service) Stop(ctx context.Context, deploymentID string) error {
	s.cancelRun(deploymentID)

	deployment, getErr := s.db.GetDeploymentByID(ctx, deploymentID)
	if getErr != nil {
		return fmt.Errorf("get deployment: %w", getErr)
	}

	s.emitLog(ctx, "stopping deployment", deployment.ID)

	if deployment.CaddyRoute != "" {
		routeID := strings.TrimSuffix(deployment.CaddyRoute, ".localhost")
		removeRouteErr := s.caddyClient.RemoveRoute(ctx, routeID)
		if removeRouteErr != nil {
			s.emitLog(ctx, fmt.Sprintf("remove caddy route: %v", removeRouteErr), deployment.ID)
		}
	}

	if deployment.ContainerName != "" {
		stopErr := s.dockerClient.StopThenRemove(ctx, deployment.ContainerName)
		if stopErr != nil {
			s.emitLog(ctx, fmt.Sprintf("remove container: %v", stopErr), deployment.ID)
		}
	}

	deployment.Status = models.StatusStopped
	updateErr := s.db.UpdateDeployment(ctx, deployment)
	if updateErr != nil {
		return fmt.Errorf("update deployment status: %w", updateErr)
	}

	// deleteErr := s.db.DeleteDeployment(ctx, deployment.ID)
	// if deleteErr != nil {
	// 	return fmt.Errorf("delete deployment record: %w", deleteErr)
	// }

	s.emitLog(ctx, "deployment stopped", deployment.ID)

	return nil
}

func (s *Service) run(ctx context.Context, deployment *models.Deployment, handle *runHandle) {
	defer func() {
		close(handle.done)
		s.runsMu.Lock()
		current, ok := s.runs[deployment.ID]
		if ok && current == handle {
			delete(s.runs, deployment.ID)
		}
		s.runsMu.Unlock()
	}()

	lw := newLogWriter(ctx, s, deployment.ID)
	s.emitLog(ctx, "cloning repo", deployment.ID)

	dir, ownerName, repoName, cloneErr := cloneRepo(ctx, deployment.GithubURL, lw)
	if cloneErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("clone repository: %v", cloneErr))
		return
	}
	defer os.RemoveAll(filepath.Dir(dir))
	lw.Flush()

	imageName := strings.ToLower(ownerName + "/" + repoName)
	deployID := strconv.FormatInt(time.Now().UnixNano(), 10)
	containerName := strings.ToLower(ownerName + "-" + repoName + "-" + deployID)
	deployment.Status = models.StatusBuilding
	deployment.ImageName = imageName
	deployment.ContainerName = containerName

	buildUpdateErr := s.db.UpdateDeployment(ctx, deployment)
	if buildUpdateErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("build state: %v", buildUpdateErr))
		return
	}
	lw.Flush()

	s.emitLog(ctx, "building image with railpack", deployment.ID)
	buildErr := buildContainerImage(ctx, dir, imageName, lw)
	if buildErr != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf("build failed: %v", buildErr))
		lw.Flush()
		return
	}
	lw.Flush()

	if ctx.Err() != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf("deployment canceled after build: %v", ctx.Err()))
		return
	}

	const networkName = "mpaas"
	networkErr := s.dockerClient.EnsureNetwork(ctx, networkName)
	if networkErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("network error: %v", networkErr))
		return
	}
	lw.Flush()

	ensureCaddyErr := s.dockerClient.EnsureCaddy(ctx, "./config/Caddyfile", networkName)
	if ensureCaddyErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("caddy error: %v", ensureCaddyErr))
		return
	}
	lw.Flush()

	caddyErr := s.caddyClient.EnsureCaddyReady(ctx, 90*time.Second)
	if caddyErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("caddy error: %v", caddyErr))
		return
	}
	lw.Flush()

	s.emitLog(ctx, "caddy container is up", deployment.ID)

	validatePortErr := validateContainerPort(deployment.ContainerPort)
	if validatePortErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("port error: %v", validatePortErr))
		return
	}
	lw.Flush()

	deployment.Status = models.StatusDeploying
	deployingUpdateErr := s.db.UpdateDeployment(ctx, deployment)
	if deployingUpdateErr != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf("deploy error: %v", deployingUpdateErr))
		return
	}

	portStr := ""
	if deployment.ContainerPort != 0 {
		portStr = strconv.Itoa(deployment.ContainerPort)
	}
	containerID, resolvedPort, runErr := s.dockerClient.RunContainer(ctx, docker.RunContainerOpts{
		ContainerPort: portStr,
		ContainerName: deployment.ContainerName,
		ImageName:     deployment.ImageName,
		DeployID:      deployID,
		NetworkName:   networkName,
	})
	deployment.ContainerPort = resolvedPort
	if runErr != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf("run container: %v", runErr))
		return
	}
	success := false
	defer func() {
		if !success {
			_ = s.dockerClient.Remove(context.Background(), containerID)
		}
	}()

	healthCtx, dCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dCancel()

	caddyCheckErr := s.dockerClient.CaddyhttpHealthCheck(healthCtx, deployment.ContainerName, deployment.ContainerPort)
	if caddyCheckErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("caddy http error: %v", caddyCheckErr))
		return
	}
	lw.Flush()

	s.emitLog(ctx, "routing via caddy", deployment.ID)

	host := strings.ToLower(ownerName+"-"+repoName) + ".localhost"
	routeID := strings.ToLower(ownerName + "-" + repoName)
	removeRouteErr := s.caddyClient.RemoveRoute(ctx, routeID)
	if removeRouteErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("caddy error: %v", removeRouteErr))
		return
	}
	lw.Flush()

	addRouteErr := s.caddyClient.AddRoute(ctx, caddy.RouteOpts{
		DeploymentID: routeID,
		Host:         host,
		Upstream:     fmt.Sprintf("%s:%d", deployment.ContainerName, deployment.ContainerPort),
	})
	if addRouteErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("add caddy route: %v", addRouteErr))
		return
	}
	lw.Flush()

	deployment.Status = models.StatusRunning
	deployment.CaddyRoute = host
	runningUpdateErr := s.db.UpdateDeployment(ctx, deployment)
	if runningUpdateErr != nil {
		s.dockerClient.StopThenRemove(ctx, deployment.ContainerName)
		s.caddyClient.RemoveRoute(ctx, routeID)
		s.failDeployment(ctx, deployment, fmt.Sprintf("running error: %v", runningUpdateErr))
		return
	}
	success = true

	s.emitLog(ctx, "deployment is live at "+deployment.CaddyRoute, deployment.ID)
}

func (s *Service) failDeployment(ctx context.Context, deployment *models.Deployment, message string) {
	ctx = context.WithoutCancel(ctx)
	s.emitLog(ctx, message, deployment.ID)
	deployment.Status = models.StatusFailed
	_ = s.db.UpdateDeployment(ctx, deployment)
}

func (s *Service) cancelRun(deploymentID string) {
	s.runsMu.Lock()
	handle, ok := s.runs[deploymentID]
	if ok {
		delete(s.runs, deploymentID)
	}
	s.runsMu.Unlock()

	if !ok {
		return
	}

	handle.cancel()

	select {
	case <-handle.done:
	case <-time.After(6 * time.Second):
	}
}

func (s *Service) emitLog(ctx context.Context, message, deploymentID string) {
	entry, err := s.db.InsertLog(ctx, message, deploymentID)
	if err != nil {
		return
	}

	s.stream.Broadcast(deploymentID, stream.Event{
		ID:        entry.ID,
		Message:   entry.Message,
		CreatedAt: entry.CreatedAt,
	})
}

func parseGitHubRepoURL(input string) (u *url.URL, ownerName, repoName string, err error) {
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err = url.ParseRequestURI(input)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", "", errInvalidGitHubRepoURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !validRepoName.MatchString(parts[0]) || !validRepoName.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return nil, "", "", errInvalidGitHubRepoURL
	}
	return u, parts[0], parts[1], nil
}

func cloneRepo(ctx context.Context, repoURL string, output io.Writer) (dir, ownerName, repoName string, err error) {
	u, ownerName, repoName, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		return "", "", "", err
	}
	parent, err := os.MkdirTemp("", "repo-*")
	if err != nil {
		return "", "", "", fmt.Errorf("mkdir temp: %w", err)
	}
	dir = filepath.Join(parent, repoName)

	cloneCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cloneCtx, "git", "clone", u.String(), dir)
	if output != nil {
		cmd.Stdout = output
		cmd.Stderr = output
	}
	err = cmd.Run()
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", "", "", fmt.Errorf("git clone: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", "", "", fmt.Errorf("read cloned dir: %w", err)
	}
	hasFiles := false
	for _, e := range entries {
		if e.Name() != ".git" {
			hasFiles = true
			break
		}
	}
	if !hasFiles {
		_ = os.RemoveAll(parent)
		return "", "", "", fmt.Errorf("cloned repo %s has no files (empty default branch?)", dir)
	}
	return dir, ownerName, repoName, nil
}

func buildContainerImage(ctx context.Context, buildDir, imageName string, output io.Writer) error {
	planPath := filepath.Join(buildDir, "railpack-plan.json")

	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(
		buildCtx,
		"railpack",
		"prepare",
		buildDir,
		"--plan-out",
		planPath,
		"--info-out",
		filepath.Join(buildDir, "railpack-info.json"),
	)
	cmd.Stdout = output
	cmd.Stderr = output

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("railpack prepare: %w", err)
	}

	cmd2 := exec.CommandContext(
		buildCtx,
		"docker", "buildx", "build",
		"--build-arg", "BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend",
		"-f", planPath,
		"--progress=rawjson",
		"-t", imageName,
		"--load",
		buildDir,
	)
	cmd2.Stdout = output
	cmd2.Stderr = output

	err = cmd2.Run()
	if err != nil {
		return fmt.Errorf("docker buildx build: %w", err)
	}
	return nil
}

func validateContainerPort(port int) error {
	if port == 0 {
		return nil
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d: must be between 1 and 65535", port)
	}

	return nil
}

// LogWriter buffers incoming bytes, splits on newlines,
// and emits each completed line via emitLog.
type logWriter struct {
	ctx          context.Context
	service      *Service
	deploymentID string
	partial      string
}

func newLogWriter(ctx context.Context, service *Service, deploymentID string) *logWriter {
	return &logWriter{
		ctx:          ctx,
		service:      service,
		deploymentID: deploymentID,
	}
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.partial += string(p)

	for {
		index := strings.IndexByte(l.partial, '\n')
		if index == -1 {
			break
		}

		line := strings.TrimSpace(l.partial[:index])
		l.partial = l.partial[index+1:]

		if line != "" {
			l.service.emitLog(l.ctx, line, l.deploymentID)
		}
	}

	return len(p), nil
}

func (l *logWriter) Flush() {
	line := strings.TrimSpace(l.partial)
	if line != "" {
		l.service.emitLog(l.ctx, line, l.deploymentID)
	}
	l.partial = ""
}
