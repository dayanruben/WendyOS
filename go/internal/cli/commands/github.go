package commands

import (
	"net/http"
	"os"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

func newGitHubAPIGetRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", githubAPIUserAgent())
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return req, nil
}

func githubAPIUserAgent() string {
	if version.Version == "" {
		return "wendy"
	}
	return "wendy/" + version.Version
}
