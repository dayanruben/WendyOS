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
