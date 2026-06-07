package oci

import (
	"testing"
)

func TestJoinGroupNamespaces_SharedIPC(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/bin/sh"})
	if err := JoinGroupNamespaces(spec, 1234, "shared-ipc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nsMap := make(map[string]string)
	for _, ns := range spec.Linux.Namespaces {
		nsMap[ns.Type] = ns.Path
	}
	if nsMap["ipc"] != "/proc/1234/ns/ipc" {
		t.Errorf("ipc namespace path = %q, want /proc/1234/ns/ipc", nsMap["ipc"])
	}
	if nsMap["network"] != "/proc/1234/ns/network" {
		t.Errorf("network namespace path = %q, want /proc/1234/ns/network", nsMap["network"])
	}
	if nsMap["uts"] != "/proc/1234/ns/uts" {
		t.Errorf("uts namespace path = %q, want /proc/1234/ns/uts", nsMap["uts"])
	}
	if nsMap["pid"] != "" {
		t.Errorf("pid namespace should not be joined, got %q", nsMap["pid"])
	}
}

func TestJoinGroupNamespaces_SharedNetwork(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/bin/sh"})
	if err := JoinGroupNamespaces(spec, 5678, "shared-network"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nsMap := make(map[string]string)
	for _, ns := range spec.Linux.Namespaces {
		nsMap[ns.Type] = ns.Path
	}
	if nsMap["network"] != "/proc/5678/ns/network" {
		t.Errorf("network namespace path = %q, want /proc/5678/ns/network", nsMap["network"])
	}
	if nsMap["uts"] != "/proc/5678/ns/uts" {
		t.Errorf("uts namespace path = %q, want /proc/5678/ns/uts", nsMap["uts"])
	}
	// ipc should remain isolated in shared-network mode
	if nsMap["ipc"] != "" {
		t.Errorf("ipc namespace should not be joined in shared-network mode, got %q", nsMap["ipc"])
	}
}

func TestJoinGroupNamespaces_Isolated(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/bin/sh"})
	if err := JoinGroupNamespaces(spec, 9999, "isolated"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ns := range spec.Linux.Namespaces {
		if ns.Path != "" {
			t.Errorf("isolated mode should not join any namespaces, but %q has path %q", ns.Type, ns.Path)
		}
	}
}

func TestJoinGroupNamespaces_ZeroPID(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/bin/sh"})
	if err := JoinGroupNamespaces(spec, 0, "shared-ipc"); err == nil {
		t.Fatal("expected error for zero PID, got nil")
	}
}

func TestJoinGroupNamespaces_NilLinux(t *testing.T) {
	spec := &Spec{}
	if err := JoinGroupNamespaces(spec, 1234, "shared-ipc"); err == nil {
		t.Fatal("expected error for nil Linux, got nil")
	}
}

func TestSharedSHMMount(t *testing.T) {
	m := SharedSHMMount("/run/wendy/shm/com.example.app")
	if m.Destination != "/dev/shm" {
		t.Errorf("Destination = %q, want /dev/shm", m.Destination)
	}
	if m.Type != "bind" {
		t.Errorf("Type = %q, want bind", m.Type)
	}
	if m.Source != "/run/wendy/shm/com.example.app" {
		t.Errorf("Source = %q, want /run/wendy/shm/com.example.app", m.Source)
	}
	found := false
	for _, opt := range m.Options {
		if opt == "rbind" {
			found = true
		}
	}
	if !found {
		t.Errorf("Options %v should contain 'rbind'", m.Options)
	}
}

func TestRemoveDefaultSHM(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/bin/sh"})
	// Verify /dev/shm exists before removal.
	hasSHM := false
	for _, m := range spec.Mounts {
		if m.Destination == "/dev/shm" {
			hasSHM = true
		}
	}
	if !hasSHM {
		t.Skip("DefaultSpec does not include /dev/shm mount; skipping")
	}
	RemoveDefaultSHM(spec)
	for _, m := range spec.Mounts {
		if m.Destination == "/dev/shm" {
			t.Error("default /dev/shm should be removed after RemoveDefaultSHM")
		}
	}
}
