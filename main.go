package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var errInvalidGitHubRepoURL = errors.New("invalid github repo url")
var validRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func main() {
	fmt.Print("enter a github repo url: ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := scanner.Text()

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir, repoName, err := cloneRepo(ctx, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error:  %v\n", err)
		os.Exit(1)
	}

	err = buildContainerImage(ctx, dir, repoName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error:  %v\n", err)
		os.Exit(1)
	}

	fmt.Println("container image: ", repoName)
}

func parseGitHubRepoURL(input string) (*url.URL, string, error) {
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.ParseRequestURI(input)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.RawQuery != "" || u.Fragment != "" {
		return nil, "", errInvalidGitHubRepoURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !validRepoName.MatchString(parts[0]) || !validRepoName.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return nil, "", errInvalidGitHubRepoURL
	}
	return u, parts[1], nil
}

func cloneRepo(ctx context.Context, repoURL string) (string, string, error) {
	u, repoName, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		return "", "", err
	}
	parent, err := os.MkdirTemp("", "repo-*")
	if err != nil {
		return "", "", fmt.Errorf("mkdir temp: %w", err)
	}
	dir := filepath.Join(parent, repoName)

	cloneCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cloneCtx, "git", "clone", u.String(), dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", "", fmt.Errorf("git clone: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", "", fmt.Errorf("read cloned dir: %w", err)
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
		return "", "", fmt.Errorf("cloned repo %s has no files (empty default branch?)", dir)
	}
	return dir, repoName, nil
}

func ensureDockerRunning(ctx context.Context) error {
	_, err := exec.CommandContext(ctx,
		"docker",
		"info",
	).CombinedOutput()

	if err != nil {
		return fmt.Errorf("docker is not running or unavailable. please restart docker to continue. %w\n", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	out, err := exec.CommandContext(
		ctx,
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
		ctx,
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
