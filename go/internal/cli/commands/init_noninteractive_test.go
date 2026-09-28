package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCommandOnPath makes PATH contain only a dir holding an executable
// named name, so isCommandAvailable(name) is true without the real tool.
func fakeCommandOnPath(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake PATH executables are shell scripts")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir)
}

// chdirTemp switches the working directory to a fresh temp dir for the test.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	return dir
}

// Without a TTY and without --assistant, `wendy init` used to open the
// assistant picker after scaffolding and fail with "picker: could not open a
// new TTY" (exit 1); a retry then failed with "wendy.json already exists".
func TestInitCommand_NonInteractiveWithoutAssistantFlagSkipsPicker(t *testing.T) {
	dir := chdirTemp(t)
	stubNonInteractive(t)
	fakeCommandOnPath(t, "claude")

	cmd := newInitCmd()
	cmd.SetArgs([]string{
		"--app-id", "demo-app",
		"--target", "wendyos",
		"--language", "python",
		"--no-extra-entitlements",
	})

	var execErr error
	stderr := captureStderr(t, func() { execErr = cmd.Execute() })
	if execErr != nil {
		t.Fatalf("Execute: %v\nstderr:\n%s", execErr, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "wendy.json")); err != nil {
		t.Fatalf("wendy.json not created: %v", err)
	}
	if n := strings.Count(stderr, "--assistant"); n != 1 {
		t.Fatalf("want exactly one line mentioning --assistant, got %d in:\n%s", n, stderr)
	}
}
