package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndpointValidation(t *testing.T) {
	for _, s := range []string{"http://192.168.1.2:80", "https://user:pass@example.com", "https://example.com?token=x", "file:///tmp/task"} {
		if _, err := NewClient(s, ""); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	for _, s := range []string{"https://agent.example/a2a", "http://127.0.0.1:8787", "http://[::1]:8787"} {
		if _, err := NewClient(s, ""); err != nil {
			t.Fatal(err)
		}
	}
}
func TestClientNeverFollowsCredentialRedirects(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, err := NewClient(source.URL, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), "GET", "/tasks", nil, nil); err == nil || called {
		t.Fatal("followed redirect", err)
	}
}
