//go:build darwin || linux || windows

package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

func TestRenderLinuxDesktopInstructions_Plain(t *testing.T) {
	out := renderLinuxDesktopInstructions("", "", "", time.Time{})
	if !strings.Contains(out, "curl -fsSL https://install.wendy.dev/agent.sh | bash") {
		t.Fatalf("plain output missing curl command:\n%s", out)
	}
	if strings.Contains(out, "WENDY_ENROLLMENT_TOKEN") {
		t.Fatalf("plain output should not mention a token:\n%s", out)
	}
	if !strings.Contains(out, "wendy device enroll") {
		t.Fatalf("plain output should point at later enrollment:\n%s", out)
	}
}

func TestRenderLinuxDesktopInstructions_Enrolled(t *testing.T) {
	exp := time.Date(2026, 7, 7, 15, 4, 5, 0, time.UTC)
	out := renderLinuxDesktopInstructions("tok-abc", "cloud.wendy.sh:443", "Acme", exp)
	for _, want := range []string{
		"WENDY_ENROLLMENT_TOKEN=tok-abc",
		"WENDY_CLOUD_HOST=cloud.wendy.sh:443",
		"install.wendy.dev/agent.sh",
		"Acme",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("enrolled output missing %q:\n%s", want, out)
		}
	}
}

func TestInstallLinuxDesktop_SkipMode_PrintsPlain(t *testing.T) {
	// preEnrollSkip must never mint a token, even if a token fn is present.
	called := false
	origTok := linuxDesktopTokenFn
	linuxDesktopTokenFn = func(_ context.Context, _ *config.AuthConfig, _ string, _ int32) (string, time.Time, error) {
		called = true
		return "should-not-be-used", time.Time{}, nil
	}
	t.Cleanup(func() { linuxDesktopTokenFn = origTok })

	out := captureStdout(t, func() {
		if err := installLinuxDesktop(context.Background(), preEnrollOptions{mode: preEnrollSkip}, ""); err != nil {
			t.Fatalf("installLinuxDesktop: %v", err)
		}
	})
	if called {
		t.Fatal("token fn must not be called in skip mode")
	}
	if !strings.Contains(out, "curl -fsSL https://install.wendy.dev/agent.sh | bash") {
		t.Fatalf("expected plain instructions:\n%s", out)
	}
}
