package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubDockerDaemonSeams saves and restores every seam ensureDockerDaemon uses,
// plus the probe bound, and shrinks the bound to d.
func stubDockerDaemonSeams(t *testing.T, d time.Duration) {
	t.Helper()
	oldRuntimes, oldWinRuntimes := darwinDockerRuntimes, windowsDockerRuntimes
	oldLookPath := dockerLookPathFn
	oldVersionOK := dockerVersionOKFn
	oldOpenRuntime := dockerOpenRuntimeFn
	oldInstallRuntime := dockerInstallRuntimeFn
	oldInteractive := isInteractiveTerminalFn
	oldTimeout := dockerVersionProbeTimeout
	t.Cleanup(func() {
		darwinDockerRuntimes, windowsDockerRuntimes = oldRuntimes, oldWinRuntimes
		dockerLookPathFn = oldLookPath
		dockerVersionOKFn = oldVersionOK
		dockerOpenRuntimeFn = oldOpenRuntime
		dockerInstallRuntimeFn = oldInstallRuntime
		isInteractiveTerminalFn = oldInteractive
		dockerVersionProbeTimeout = oldTimeout
	})
	dockerVersionProbeTimeout = d
}

// A wedged Docker daemon (the socket accepts the connection but never
// answers) used to stall `wendy run`/`build` forever: the `docker version`
// probe ran under the command's context, which has no deadline. It must now
// fail fast with an error that says Docker isn't responding — and, even in a
// terminal, not offer to open or install a runtime that is already running.
func TestEnsureDockerDaemon_HungDaemonFailsWithClearError(t *testing.T) {
	for _, hostOS := range []dockerHostOS{dockerHostOSDarwin, dockerHostOSWindows, "linux"} {
		t.Run(string(hostOS), func(t *testing.T) {
			stubDockerDaemonSeams(t, 100*time.Millisecond)
			appPath := filepath.Join(t.TempDir(), "Docker.app")
			if err := os.MkdirAll(appPath, 0o755); err != nil {
				t.Fatal(err)
			}
			darwinDockerRuntimes = []dockerRuntime{{name: "Docker Desktop", app: appPath}}
			windowsDockerRuntimes = []dockerRuntime{{name: "Docker Desktop", app: appPath}}
			dockerLookPathFn = func(string) (string, error) { return "/usr/local/bin/docker", nil }
			dockerVersionOKFn = func(ctx context.Context) bool {
				<-ctx.Done() // never answers
				return false
			}
			dockerOpenRuntimeFn = func(context.Context, string) error {
				t.Fatal("must not open a runtime whose daemon is running but hung")
				return nil
			}
			dockerInstallRuntimeFn = func(context.Context) error {
				t.Fatal("must not install anything")
				return nil
			}
			isInteractiveTerminalFn = func() bool { return true }
			stubConfirmFn(t, func(q string) bool {
				t.Fatalf("must not prompt (%q) for a hung daemon", q)
				return false
			})

			start := time.Now()
			err := ensureDockerDaemonForHostOS(context.Background(), hostOS)
			if err == nil || !strings.Contains(err.Error(), "not responding") {
				t.Fatalf("err = %v, want an error saying the Docker daemon is not responding", err)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Fatalf("took %s; want the probe bounded by dockerVersionProbeTimeout", elapsed)
			}
		})
	}
}

func TestDockerDaemonReady(t *testing.T) {
	cases := []struct {
		name      string
		probe     func(ctx context.Context) bool
		cancelCtx bool
		wantReady bool
		wantHung  bool
		wantErr   error
	}{
		{name: "answers", probe: func(context.Context) bool { return true }, wantReady: true},
		{name: "slow but healthy", probe: func(context.Context) bool { time.Sleep(20 * time.Millisecond); return true }, wantReady: true},
		{name: "not running fails fast", probe: func(context.Context) bool { return false }},
		{name: "hung", probe: func(ctx context.Context) bool { <-ctx.Done(); return false }, wantHung: true},
		// Ctrl-C while probing is the user stopping, not a hung daemon.
		{name: "caller cancelled", probe: func(ctx context.Context) bool { <-ctx.Done(); return false }, cancelCtx: true, wantErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubDockerDaemonSeams(t, 200*time.Millisecond)
			dockerVersionOKFn = tc.probe
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelCtx {
				time.AfterFunc(20*time.Millisecond, cancel)
			}
			ready, err := dockerDaemonReady(ctx)
			if ready != tc.wantReady {
				t.Errorf("ready = %v, want %v", ready, tc.wantReady)
			}
			if gotHung := err != nil && strings.Contains(err.Error(), "not responding"); gotHung != tc.wantHung {
				t.Errorf("err = %v, want not-responding error: %v", err, tc.wantHung)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if !tc.wantHung && tc.wantErr == nil && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

// The real probe (`docker version` via exec) is bounded too: a docker CLI
// that never returns is killed at the deadline.
func TestDockerDaemonReady_HungCLIIsKilled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake docker is a shell script")
	}
	stubDockerDaemonSeams(t, 200*time.Millisecond)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	start := time.Now()
	ready, err := dockerDaemonReady(context.Background())
	if ready || err == nil || !strings.Contains(err.Error(), "not responding") {
		t.Fatalf("dockerDaemonReady = %v, %v; want a not-responding error", ready, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s; want the hung docker CLI killed at the probe deadline", elapsed)
	}
}
