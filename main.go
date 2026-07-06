package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errInvalidGitHubRepoURL = errors.New("invalid github repo url")
var validRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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

func cloneRepo(ctx context.Context, repoURL string) (dir, ownerName, repoName string, err error) {
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

func ensureDockerRunning(ctx context.Context) error {
	_, err := exec.CommandContext(ctx,
		"docker",
		"info",
	).CombinedOutput()

	if err != nil {
		return fmt.Errorf("docker not running or unavailable: %w", err)
	}
	return nil
}

func ensureNetwork(ctx context.Context, networkName string) error {
	if err := exec.CommandContext(ctx, "docker", "network", "inspect", networkName).Run(); err == nil {
		return nil
	}
	if out, err := exec.CommandContext(ctx, "docker", "network", "create", networkName).CombinedOutput(); err != nil {
		return fmt.Errorf("create network: %w\n%s", err, out)
	}
	return nil
}

func buildContainerImage(ctx context.Context, buildDir, imageName string) error {
	err := ensureDockerRunning(ctx)
	if err != nil {
		return err
	}

	fmt.Println("building container image...")

	planPath := filepath.Join(buildDir, "railpack-plan.json")

	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	out, err := exec.CommandContext(
		buildCtx,
		"railpack",
		"prepare",
		buildDir,
		"--plan-out",
		planPath,
		"--info-out",
		filepath.Join(buildDir, "railpack-info.json"),
	).CombinedOutput()

	if err != nil {
		return fmt.Errorf("railpack prepare: %w\n%s", err, out)
	}

	out, err = exec.CommandContext(
		buildCtx,
		"docker", "buildx", "build",
		"--build-arg", "BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend",
		"-f", planPath,
		"--progress=rawjson",
		"-t", imageName,
		"--load",
		buildDir,
	).CombinedOutput()

	if err != nil {
		return fmt.Errorf("docker buildx build: %w\n%s", err, out)
	}
	return nil
}

func runContainer(ctx context.Context, containerPort, containerName, imageName, network string) (string, error) {
	if containerPort == "" {
		containerPort = "8080"
	}

	out, err := exec.CommandContext(ctx,
		"docker", "run", "-d",
		"--name", containerName,
		"--network", network,
		"-e", "PORT="+containerPort,
		"-p", "0:"+containerPort,
		imageName,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker run: %w\n%s", err, out)
	}

	format := fmt.Sprintf(
		"{{(index (index .NetworkSettings.Ports %q) 0).HostPort}}",
		containerPort+"/tcp",
	)

	out, err = exec.CommandContext(
		ctx,
		"docker",
		"inspect",
		"-f",
		format,
		containerName,
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w\n%s", err, out)
	}

	return strings.TrimSpace(string(out)), nil
}

func validateContainerPort(port string) error {
	if port == "" {
		return nil
	}

	n, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port %q: not a number", port)
	}

	if n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q: must be between 1 and 65535", port)
	}

	return nil
}

func httpHealthCheck(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("HTTP request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("no HTTP response from %s: %w", url, ctx.Err())
		case <-ticker.C:
		}
	}
}

func run() error {
	fmt.Print("enter a github repo url: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	err := scanner.Err()
	if err != nil {
		return fmt.Errorf("error reading input: %w\n", err)
	}

	input := scanner.Text()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	err = ensureDockerRunning(ctx)
	if err != nil {
		return err
	}

	dir, ownerName, repoName, err := cloneRepo(ctx, input)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(dir))

	imageName := strings.ToLower(ownerName + "/" + repoName)
	deployID := strconv.FormatInt(time.Now().UnixNano(), 10)
	containerName := strings.ToLower(ownerName + "-" + repoName + "-" + deployID)

	const networkName = "deploy-net"
	err = ensureNetwork(ctx, networkName)
	if err != nil {
		return err
	}

	err = buildContainerImage(ctx, dir, imageName)
	if err != nil {
		return err
	}

	fmt.Print("enter port: ")

	port := bufio.NewScanner(os.Stdin)
	port.Scan()
	if err := port.Err(); err != nil {
		return fmt.Errorf("error reading input: %w\n", err)
	}

	containerPort := port.Text()
	err = validateContainerPort(containerPort)
	if err != nil {
		return err
	}

	hostPort, err := runContainer(ctx, containerPort, containerName, imageName, networkName)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort("127.0.0.1", hostPort)
	detectCtx, dCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dCancel()

	url := fmt.Sprintf("http://127.0.0.1:%s/", hostPort)
	err = httpHealthCheck(detectCtx, url)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	fmt.Printf("container %q is listening on %s\n", containerName, addr)
	return nil
}
