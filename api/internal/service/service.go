package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amxrac/mpaas/api/internal/caddy"
	"github.com/amxrac/mpaas/api/internal/db"
	"github.com/amxrac/mpaas/api/internal/docker"
	"github.com/amxrac/mpaas/api/internal/models"
	"github.com/amxrac/mpaas/api/internal/stream"
)

var errInvalidGitHubRepoURL = errors.New("invalid github repo url")
var ErrInvalidPort = errors.New("invalid port")
var ErrInvalidEnvVar = errors.New("invalid env var")
var ErrInvalidRootDir = errors.New("invalid root directory")
var errContainerNotRunning = errors.New("container is not running")
var validRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var validOwnerName = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)
var vaildEnvVar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var validRootDir = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var invalidHostChars = regexp.MustCompile(`[^a-z0-9-]+`)

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

type DeployOpts struct {
	GithubURL string
	Port      int
	Env       map[string]string
	RootDir   string
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
// and starts run() on a single goroutine.
func (s *Service) Deploy(ctx context.Context, opts DeployOpts) (*models.Deployment, error) {
	err := validateContainerPort(opts.Port)
	if err != nil {
		return nil, err
	}

	err = validateEnvVars(opts.Env)
	if err != nil {
		return nil, err
	}

	rootDir, err := normalizeRootDir(opts.RootDir)
	if err != nil {
		return nil, err
	}

	deployment := &models.Deployment{
		GithubURL:     opts.GithubURL,
		Status:        models.StatusPending,
		ContainerPort: opts.Port,
		Env:           opts.Env,
		RootDir:       rootDir,
	}

	err = s.db.InsertDeployment(ctx, deployment)
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

	s.emitLog(ctx, "deployment stopped", deployment.ID)

	return nil
}

func (s *Service) Delete(ctx context.Context, deploymentID string) error {
	err := s.Stop(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("stop deployment before delete: %w", err)
	}

	err = s.db.DeleteDeployment(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("delete deployment record: %w", err)
	}
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

	buildDir := filepath.Join(dir, filepath.FromSlash(deployment.RootDir))
	info, statErr := os.Stat(buildDir)
	if statErr != nil || !info.IsDir() {
		s.failDeployment(ctx, deployment,
			fmt.Sprintf("root directory %q not found in repository", deployment.RootDir))
		return
	}

	label := hostLabel(ownerName, repoName, deployment.RootDir)
	imageName := strings.ToLower(ownerName) + "/" + hostLabel(repoName, deployment.RootDir)
	deployID := strconv.FormatInt(time.Now().UnixNano(), 10)
	containerName := label + "-" + deployID
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
	buildErr := buildContainerImage(ctx, buildDir, imageName, lw)
	if buildErr != nil {
		lw.Flush()
		s.failDeployment(ctx, deployment, fmt.Sprintf("build failed: %v", buildErr))
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
		Env:           deployment.Env,
	})
	deployment.ContainerPort = resolvedPort
	if runErr != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf("run container: %v", runErr))
		return
	}
	success := false
	defer func() {
		if !success {
			_ = s.dockerClient.StopThenRemove(context.Background(), containerID)
		}
	}()

	upstream := fmt.Sprintf("%s:%d", deployment.ContainerName, deployment.ContainerPort)
	s.emitLog(ctx, fmt.Sprintf("waiting for app on port %d", deployment.ContainerPort), deployment.ID)

	healthCtx, healthCancel := context.WithTimeout(ctx, 60*time.Second)
	healthErr := httpHealthCheck(healthCtx, "http://"+upstream+"/")
	healthCancel()
	if healthErr != nil {
		s.failDeployment(ctx, deployment, fmt.Sprintf(
			"health check failed: %v; app must listen on $PORT (%d), or redeploy with the port it listens on",
			healthErr, deployment.ContainerPort))
		return
	}

	s.emitLog(ctx, "routing via caddy", deployment.ID)

	host := label + ".localhost"
	routeID := label
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
		Upstream:     upstream,
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
		s.caddyClient.RemoveRoute(context.WithoutCancel(ctx), routeID)
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

func ParseGitHubRepoURL(input string) (u *url.URL, ownerName, repoName string, err error) {
	maxOwnerLen := 39
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err = url.ParseRequestURI(input)
	if err != nil ||
		u.Scheme != "https" ||
		!strings.EqualFold(u.Host, "github.com") ||
		u.RawQuery != "" ||
		u.ForceQuery ||
		u.Fragment != "" {
		return nil, "", "", errInvalidGitHubRepoURL
	}

	if strings.Contains(u.EscapedPath(), "%") {
		return nil, "", "", errInvalidGitHubRepoURL
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return nil, "", "", errInvalidGitHubRepoURL
	}
	owner, repo := parts[0], parts[1]
	if len(owner) > maxOwnerLen || !validOwnerName.MatchString(owner) {
		return nil, "", "", errInvalidGitHubRepoURL
	}

	if !validRepoName.MatchString(repo) || repo == "." || repo == ".." {
		return nil, "", "", errInvalidGitHubRepoURL
	}

	if strings.HasSuffix(strings.ToLower(repo), ".git") {
		return nil, "", "", errInvalidGitHubRepoURL
	}

	return u, owner, repo, nil
}

func hostLabel(parts ...string) string {
	s := strings.ToLower(strings.Join(parts, "-"))
	s = invalidHostChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}

	if s == "" {
		s = "app"
	}
	return s
}

func normalizeRootDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}

	if strings.HasPrefix(dir, "/") {
		return "", fmt.Errorf("%w: path must be a relative, like apps/web", ErrInvalidRootDir)
	}

	clean := path.Clean(dir)
	if clean == "." {
		return "", nil
	}

	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: must stay inside the repository", ErrInvalidRootDir)
	}
	return clean, nil
}

func cloneRepo(ctx context.Context, repoURL string, output io.Writer) (dir, ownerName, repoName string, err error) {
	u, ownerName, repoName, err := ParseGitHubRepoURL(repoURL)
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
		"--progress=plain",
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
		return fmt.Errorf("%w %d: must be between 1 and 65535", ErrInvalidPort, port)
	}

	return nil
}

func validateEnvVars(env map[string]string) error {
	maxEnvVars := 50
	maxEnvValueLen := 4096

	if len(env) > maxEnvVars {
		return fmt.Errorf("%w: %d variables at most", ErrInvalidEnvVar, maxEnvVars)
	}

	for k, v := range env {
		if !vaildEnvVar.MatchString(k) {
			return fmt.Errorf("%w: key %q must match [A-Za-z_][A-Za-z0-9_]*", ErrInvalidEnvVar, k)
		}

		if k == "PORT" {
			return fmt.Errorf("%w: use the port field to set PORT", ErrInvalidEnvVar)
		}

		if len(v) > maxEnvValueLen {
			return fmt.Errorf("%w: value for %q exceeds %d bytes", ErrInvalidEnvVar, k, maxEnvValueLen)
		}

		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("%w: value for %q contains a NUL byte", ErrInvalidEnvVar, k)
		}
	}
	return nil
}

func httpHealthCheck(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	notFound := 0
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("health request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err

			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				notFound++
				if notFound >= 3 {
					return errContainerNotRunning
				} else {
					notFound = 0
				}
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("no healthy response from %s: %w (last error: %v)", url, ctx.Err(), lastErr)
		case <-ticker.C:
		}

	}

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
