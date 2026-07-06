//go:build darwin || linux

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThorElevationDecision(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		euid        int
		hasUdevRule bool
		interactive bool
		want        thorElevationAction
	}{
		{"root proceeds (darwin)", "darwin", 0, false, true, thorElevationProceed},
		{"root proceeds (linux, no rule)", "linux", 0, false, false, thorElevationProceed},
		{"darwin non-root interactive re-execs", "darwin", 501, false, true, thorElevationReexec},
		{"darwin non-root non-interactive fails early", "darwin", 501, false, false, thorElevationFailEarly},
		{"darwin ignores udev rule", "darwin", 501, true, true, thorElevationReexec},
		{"linux with rule proceeds", "linux", 1000, true, true, thorElevationProceed},
		{"linux no rule interactive re-execs", "linux", 1000, false, true, thorElevationReexec},
		{"linux no rule non-interactive fails early", "linux", 1000, false, false, thorElevationFailEarly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := thorElevationDecision(tt.goos, tt.euid, tt.hasUdevRule, tt.interactive)
			if got != tt.want {
				t.Errorf("thorElevationDecision(%q,%d,%v,%v) = %v, want %v",
					tt.goos, tt.euid, tt.hasUdevRule, tt.interactive, got, tt.want)
			}
		})
	}
}

func TestHasWendyJetsonUdevRule(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "70-wendy-jetson.rules")
	if err := os.WriteFile(present, []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(dir, "does-not-exist.rules")

	if !hasWendyJetsonUdevRule([]string{absent, present}) {
		t.Error("want true when at least one rule path exists")
	}
	if hasWendyJetsonUdevRule([]string{absent}) {
		t.Error("want false when no rule path exists")
	}
	if hasWendyJetsonUdevRule(nil) {
		t.Error("want false for empty path list")
	}
}

func TestHasDeviceTypeFlag(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"install", "--device-type", "jetson-agx-thor"}, true},
		{[]string{"install", "--device-type=jetson-agx-thor"}, true},
		{[]string{"install", "--nightly"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := hasDeviceTypeFlag(tt.args); got != tt.want {
			t.Errorf("hasDeviceTypeFlag(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestBuildSudoReexecArgs(t *testing.T) {
	self := "/usr/local/bin/wendy"

	// Interactive picker case: no --device-type in original args -> inject it.
	got := buildSudoReexecArgs(self, []string{"install", "--nightly"})
	if got[0] != thorSudoPreserveEnv {
		t.Errorf("arg[0] = %q, want preserve-env %q", got[0], thorSudoPreserveEnv)
	}
	if got[1] != self {
		t.Errorf("arg[1] = %q, want self %q", got[1], self)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "install --nightly") {
		t.Errorf("original args not preserved: %v", got)
	}
	if !strings.Contains(joined, "--device-type "+thorDeviceType) {
		t.Errorf("device-type not injected: %v", got)
	}

	// Flag case: --device-type already present -> do not duplicate.
	got = buildSudoReexecArgs(self, []string{"install", "--device-type", thorDeviceType, "--force"})
	if n := strings.Count(strings.Join(got, " "), thorDeviceType); n != 1 {
		t.Errorf("device-type duplicated (%d occurrences): %v", n, got)
	}

	// Equals form is also recognized as present.
	got = buildSudoReexecArgs(self, []string{"install", "--device-type=" + thorDeviceType})
	if n := strings.Count(strings.Join(got, " "), "--device-type"); n != 1 {
		t.Errorf("device-type duplicated for equals form: %v", got)
	}
}
