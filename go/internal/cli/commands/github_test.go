package commands

import (
	"net/http"
	"testing"
)

func TestNewGitHubAPIGetRequestWithoutToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	req, err := newGitHubAPIGetRequest(githubReleasesURL)
	if err != nil {
		t.Fatalf("newGitHubAPIGetRequest: %v", err)
	}

	if req.Method != http.MethodGet {
		t.Fatalf("method = %q; want %q", req.Method, http.MethodGet)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q; want empty", got)
	}
	if got := req.Header.Get("User-Agent"); got == "" {
		t.Fatal("User-Agent should be set")
	}
}

func TestNewGitHubAPIGetRequestWithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret-token")

	req, err := newGitHubAPIGetRequest(githubReleasesURL)
	if err != nil {
		t.Fatalf("newGitHubAPIGetRequest: %v", err)
	}

	if got, want := req.Header.Get("Authorization"), "Bearer secret-token"; got != want {
		t.Fatalf("Authorization = %q; want %q", got, want)
	}
}
