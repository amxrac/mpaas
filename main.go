package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func main() {
	fmt.Print("enter a github repo url: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := scanner.Text()
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
		os.Exit(1)
	}
	if isGitHubRepoURL(input) {
		fmt.Println("valid github repo url.")
	} else {
		fmt.Println("invalid repo url.")
	}

}

func isGitHubRepoURL(input string) bool {
	if !strings.HasPrefix(input, "https://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 2 &&
		parts[0] != "" &&
		parts[1] != ""
}
