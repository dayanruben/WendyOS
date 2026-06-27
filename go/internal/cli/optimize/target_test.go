package optimize

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestResolveArch(t *testing.T) {
	if got := resolveArch(""); got != "arm64" {
		t.Fatalf("resolveArch(\"\") = %q, want arm64", got)
	}
	if got := resolveArch("amd64"); got != "amd64" {
		t.Fatalf("resolveArch(\"amd64\") = %q, want amd64", got)
	}
}

func TestDiscoverSingleDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", "FROM python:3.12-slim\n")
	writeFile(t, dir, "requirements.txt", "torch==2.3.0+cpu\n")

	targets, err := DiscoverTargets(dir, nil, "arm64")
	if err != nil {
		t.Fatalf("DiscoverTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("got %d targets, want 1", len(targets))
	}
	tg := targets[0]
	if tg.Kind != KindDockerfile {
		t.Fatalf("kind = %v, want KindDockerfile", tg.Kind)
	}
	if tg.Dockerfile == nil || len(tg.Dockerfile.Instructions) == 0 {
		t.Fatalf("Dockerfile not parsed")
	}
	if tg.Requirements == nil || len(tg.Requirements.Packages) != 1 {
		t.Fatalf("requirements not attached")
	}
	if tg.Arch != "arm64" {
		t.Fatalf("arch = %q, want arm64", tg.Arch)
	}
}

func TestDiscoverNativeSwift(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", "// swift-tools-version:6.0\n")
	targets, err := DiscoverTargets(dir, nil, "arm64")
	if err != nil {
		t.Fatalf("DiscoverTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].Kind != KindNativeSwift {
		t.Fatalf("targets = %+v, want one KindNativeSwift", targets)
	}
}

func TestDiscoverNothing(t *testing.T) {
	dir := t.TempDir()
	targets, err := DiscoverTargets(dir, nil, "arm64")
	if err != nil {
		t.Fatalf("DiscoverTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("got %d targets, want 0", len(targets))
	}
}
