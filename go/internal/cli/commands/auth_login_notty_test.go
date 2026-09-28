package commands

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func shrinkBrowserLoginTimeout(t *testing.T) {
	t.Helper()
	prev := browserLoginTimeout
	browserLoginTimeout = 100 * time.Millisecond
	t.Cleanup(func() { browserLoginTimeout = prev })
}

// stubOpenBrowser replaces openBrowser and returns a func reporting the URLs
// it was asked to open.
func stubOpenBrowser(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	prev := openBrowser
	openBrowser = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, u)
		return nil
	}
	t.Cleanup(func() { openBrowser = prev })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), opened...)
	}
}

const testLoginURLPrefix = "https://cloud.example.invalid/cli-auth?redirect_uri="

// Without a TTY the legacy login used to launch a browser and then wait
// forever for a callback nobody would deliver.
func TestPerformLogin_NonInteractivePrintsURLAndTimesOut(t *testing.T) {
	stubNonInteractive(t)
	shrinkBrowserLoginTimeout(t)
	opened := stubOpenBrowser(t)

	var err error
	out := captureStdout(t, func() {
		err = performLogin(context.Background(), "https://cloud.example.invalid", "grpc.example.invalid:443")
	})

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened a browser without a TTY: %v", got)
	}
	var urlLine bool
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, testLoginURLPrefix) && line == strings.TrimSpace(line) {
			urlLine = true
		}
	}
	if !urlLine {
		t.Fatalf("want the login URL alone on its own line, got:\n%s", out)
	}
}

// With a TTY nothing changes: the browser is opened with the login URL.
func TestPerformLogin_InteractiveOpensBrowser(t *testing.T) {
	stubInteractive(t)
	shrinkBrowserLoginTimeout(t)
	opened := stubOpenBrowser(t)

	var err error
	_ = captureStdout(t, func() {
		err = performLogin(context.Background(), "https://cloud.example.invalid", "grpc.example.invalid:443")
	})

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	got := opened()
	if len(got) != 1 || !strings.HasPrefix(got[0], testLoginURLPrefix) {
		t.Fatalf("opened = %v, want exactly the login URL", got)
	}
}

// Ctrl-C while waiting for the browser must end the wait at once, in either
// mode — not after browserLoginTimeout.
func TestPerformLogin_CancelEndsWaitImmediately(t *testing.T) {
	stubNonInteractive(t)
	stubOpenBrowser(t)
	// browserLoginTimeout stays at its 5-minute default: only the cancel can end this quickly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	var err error
	_ = captureStdout(t, func() {
		err = performLogin(ctx, "https://cloud.example.invalid", "grpc.example.invalid:443")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s to honour the cancel", elapsed)
	}
}
