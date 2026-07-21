package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/amxrac/mpaas/internal/caddy"
	"github.com/amxrac/mpaas/internal/docker"
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

func buildContainerImage(ctx context.Context, buildDir, imageName string) error {
	fmt.Println("building app container image...")

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
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("enter a github repo url: ")

	if !scanner.Scan() {
		err := scanner.Err()
		if err != nil {
			return fmt.Errorf("read repo URL: %w", err)
		}
		return io.EOF
	}

	input := scanner.Text()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	d, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}
	defer d.Close()

	err = d.Ping(ctx)
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

	err = d.EnsureNetwork(ctx, networkName)
	if err != nil {
		return err
	}

	err = d.EnsureCaddy(ctx, "./config/Caddyfile", networkName)
	if err != nil {
		return err
	}

	c := caddy.NewClient("http://localhost:2019")

	err = c.EnsureCaddyReady(ctx, 90*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("caddy container is up\n")

	err = buildContainerImage(ctx, dir, imageName)
	if err != nil {
		return err
	}

	fmt.Print("enter app port: ")

	if !scanner.Scan() {
		err := scanner.Err()
		if err != nil {
			return fmt.Errorf("read app port: %w", err)
		}
	}

	containerPort := scanner.Text()
	err = validateContainerPort(containerPort)
	if err != nil {
		return err
	}

	containerID, hostPort, err := d.RunContainer(ctx, containerPort, containerName, imageName, deployID, networkName)
	if err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			_ = d.Remove(context.Background(), containerID)
		}
	}()

	detectCtx, dCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dCancel()

	url := fmt.Sprintf("http://127.0.0.1:%s/", hostPort)
	err = httpHealthCheck(detectCtx, url)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	success = true

	fmt.Printf("container %q is listening on %s\n", containerName, url)

	host := strings.ToLower(ownerName+"-"+repoName) + ".localhost"
	port, err := strconv.Atoi(containerPort)

	if err != nil {
		return fmt.Errorf("parse container port: %w", err)
	}

	deploymentID := strings.ToLower(ownerName + "-" + repoName)

	err = c.RemoveRoute(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("remove caddy route: %w", err)
	}

	err = c.AddRoute(ctx, caddy.RouteOpts{
		DeploymentID: deploymentID,
		Host:         host,
		Upstream:     fmt.Sprintf("%s:%d", containerName, port),
	})

	if err != nil {
		return fmt.Errorf("add caddy route: %w", err)
	}

	fmt.Printf("routed at http://%s\n", host)

	return nil
}
