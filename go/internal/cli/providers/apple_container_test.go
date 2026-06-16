package providers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

func TestAppleContainerSupportedHost(t *testing.T) {
	if !appleContainerSupportedHost("darwin", "arm64") {
		t.Fatal("darwin/arm64 should support Apple Container")
	}
	for _, tc := range []struct {
		goos   string
		goarch string
	}{
		{"darwin", "amd64"},
		{"linux", "arm64"},
		{"windows", "arm64"},
	} {
		if appleContainerSupportedHost(tc.goos, tc.goarch) {
			t.Fatalf("%s/%s should not support Apple Container", tc.goos, tc.goarch)
		}
	}
}

func TestAppleContainerInspectHasManagedLabel(t *testing.T) {
	managed := []byte(`{"id":"app","configuration":{"labels":{"wendy.managed":"true"}}}`)
	if !appleContainerInspectHasManagedLabel(managed) {
		t.Fatal("expected managed label to be detected")
	}

	unmanaged := []byte(`{"id":"app","configuration":{"labels":{"other":"true"}}}`)
	if appleContainerInspectHasManagedLabel(unmanaged) {
		t.Fatal("unexpected managed label")
	}
}

func TestAppleContainerListInfos(t *testing.T) {
	got := appleContainerListInfos([]byte(`[
		{"id":"app","image":"app:latest","state":"running"},
		{"ID":"other","Image":"other:latest","State":"stopped","Status":"exited"},
		{
			"id":"nested",
			"configuration":{"image":{"reference":"nested:latest"}},
			"status":{"state":"running"}
		}
	]`))
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Name != "app" || got[0].Image != "app:latest" || got[0].State != "running" {
		t.Fatalf("first entry = %+v", got[0])
	}
	if got[1].Name != "other" || got[1].Status != "exited" {
		t.Fatalf("second entry = %+v", got[1])
	}
	if got[2].Name != "nested" || got[2].Image != "nested:latest" || got[2].State != "running" || got[2].Status != "running" {
		t.Fatalf("third entry = %+v", got[2])
	}
}

func TestAppleContainerBuildWithDockerfileUsesContainerBuild(t *testing.T) {
	restore := stubAppleContainerHost(t)
	defer restore()

	logFile := filepath.Join(t.TempDir(), "commands.log")
	oldCommand := appleContainerCommandContext
	oldLookPath := appleContainerLookPath
	t.Cleanup(func() {
		appleContainerCommandContext = oldCommand
		appleContainerLookPath = oldLookPath
	})
	appleContainerCommandContext = fakeAppleContainerCommandContext(logFile)
	appleContainerLookPath = func(file string) (string, error) {
		if file == "container" {
			return "/usr/local/bin/container", nil
		}
		return "", errors.New("not found")
	}

	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile.prod")
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildContext, err := appleContainerBuildContextPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedDockerfile, err := filepath.EvalSymlinks(dockerfile)
	if err != nil {
		t.Fatal(err)
	}

	p := &AppleContainerProvider{}
	app, err := p.BuildWithDockerfile(context.Background(), models.ExternalDevice{CPUArchitecture: "arm64"}, dir, "MyApp", "docker", "Dockerfile.prod", false)
	if err != nil {
		t.Fatalf("BuildWithDockerfile: %v", err)
	}
	if app.ProviderKey != ProviderKeyAppleContainer {
		t.Fatalf("ProviderKey = %q", app.ProviderKey)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{
		"container\x00--version\n",
		"container\x00system\x00status\n",
		"container\x00build\x00--platform\x00linux/arm64\x00-t\x00myapp:latest\x00-f\x00" + resolvedDockerfile + "\x00" + buildContext + "\n",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("command log missing %q in:\n%s", want, log)
		}
	}
}

func TestAppleContainerBuildWithContainerfileUsesContainerBuild(t *testing.T) {
	restore := stubAppleContainerHost(t)
	defer restore()

	logFile := filepath.Join(t.TempDir(), "commands.log")
	oldCommand := appleContainerCommandContext
	oldLookPath := appleContainerLookPath
	t.Cleanup(func() {
		appleContainerCommandContext = oldCommand
		appleContainerLookPath = oldLookPath
	})
	appleContainerCommandContext = fakeAppleContainerCommandContext(logFile)
	appleContainerLookPath = func(file string) (string, error) {
		if file == "container" {
			return "/usr/local/bin/container", nil
		}
		return "", errors.New("not found")
	}

	dir := t.TempDir()
	containerfile := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(containerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buildContext, err := appleContainerBuildContextPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedContainerfile, err := filepath.EvalSymlinks(containerfile)
	if err != nil {
		t.Fatal(err)
	}

	p := &AppleContainerProvider{}
	if !p.CanBuild(dir) {
		t.Fatal("CanBuild = false, want true for Containerfile")
	}
	if _, err := p.Build(context.Background(), models.ExternalDevice{CPUArchitecture: "arm64"}, dir, "MyApp", false); err != nil {
		t.Fatalf("Build: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "container\x00build\x00--platform\x00linux/arm64\x00-t\x00myapp:latest\x00-f\x00" + resolvedContainerfile + "\x00" + buildContext + "\n"
	if !strings.Contains(string(data), want) {
		t.Fatalf("command log missing %q in:\n%s", want, string(data))
	}
}

func TestAppleContainerBuildContextUsesTmpAliasWhenAvailable(t *testing.T) {
	restore := stubAppleContainerHost(t)
	defer restore()

	dir, err := os.MkdirTemp("/tmp", "wendy-apple-context.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	privateDir := "/private" + dir
	if !sameFilePath(dir, privateDir) {
		t.Skip("/private/tmp is not an alias for /tmp on this host")
	}

	got, err := appleContainerBuildContextPath(privateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("build context = %q, want %q", got, dir)
	}
}

func TestAppleContainerCheckRequirementsRejectsUnsupportedHost(t *testing.T) {
	oldGOOS := appleContainerHostGOOS
	oldGOARCH := appleContainerHostGOARCH
	t.Cleanup(func() {
		appleContainerHostGOOS = oldGOOS
		appleContainerHostGOARCH = oldGOARCH
	})
	appleContainerHostGOOS = func() string { return "linux" }
	appleContainerHostGOARCH = func() string { return "arm64" }

	err := (&AppleContainerProvider{}).CheckRequirements(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Apple silicon Mac") {
		t.Fatalf("CheckRequirements error = %v, want Apple silicon Mac diagnostic", err)
	}
}

func stubAppleContainerHost(t *testing.T) func() {
	t.Helper()
	oldGOOS := appleContainerHostGOOS
	oldGOARCH := appleContainerHostGOARCH
	appleContainerHostGOOS = func() string { return "darwin" }
	appleContainerHostGOARCH = func() string { return "arm64" }
	return func() {
		appleContainerHostGOOS = oldGOOS
		appleContainerHostGOARCH = oldGOARCH
	}
}

func fakeAppleContainerCommandContext(logFile string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmdArgs := []string{"-test.run=TestAppleContainerHelperProcess", "--", name}
		cmdArgs = append(cmdArgs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_APPLE_CONTAINER_HELPER_PROCESS=1",
			"APPLE_CONTAINER_HELPER_LOG="+logFile,
		)
		return cmd
	}
}

func TestAppleContainerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_APPLE_CONTAINER_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if logFile := os.Getenv("APPLE_CONTAINER_HELPER_LOG"); logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(strings.Join(args, "\x00") + "\n")
			_ = f.Close()
		}
	}
	if len(args) >= 2 && args[0] == "container" && args[1] == "--version" {
		_, _ = os.Stdout.WriteString("container 1.0.0\n")
		os.Exit(0)
	}
	if len(args) >= 3 && args[0] == "container" && args[1] == "system" && args[2] == "status" {
		os.Exit(0)
	}
	if len(args) >= 2 && args[0] == "container" && args[1] == "build" {
		os.Exit(0)
	}
	os.Exit(1)
}
