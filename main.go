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

var errInvalidGitHubRepoURL = errors.New("invalid github repository url")
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
	ctx := context.Background()
	dir, err := cloneRepo(ctx, input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error:  %v\n", err)
		os.Exit(1)
	}
	fmt.Println("repo: ", dir)
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

func cloneRepo(ctx context.Context, repoURL string) (string, error) {
	u, repoName, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		return "", err
	}
	parent, err := os.MkdirTemp("", "repo-*")
	if err != nil {
		return "", fmt.Errorf("mkdir temp: %w", err)
	}
	dir := filepath.Join(parent, repoName)

	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", u.String(), dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", fmt.Errorf("git clone: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		_ = os.RemoveAll(parent)
		return "", fmt.Errorf("read cloned dir: %w", err)
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
		return "", fmt.Errorf("cloned repo %s has no files (empty default branch?)", dir)
	}
	return dir, nil
}
