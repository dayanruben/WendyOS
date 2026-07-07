//go:build darwin || linux || windows

package commands

import (
	"strings"
	"testing"
	"time"
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
