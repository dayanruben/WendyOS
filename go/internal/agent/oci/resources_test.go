package oci

import (
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

func TestApplyResourceLimits_AllFields(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/app"})
	limits := &appconfig.ResourceLimits{Memory: "512Mi", CPUs: "1.5", PIDs: 256}

	if err := ApplyResourceLimits(spec, limits); err != nil {
		t.Fatalf("ApplyResourceLimits: %v", err)
	}

	res := spec.Linux.Resources
	if res.Memory == nil || res.Memory.Limit == nil || *res.Memory.Limit != 512*1024*1024 {
		t.Errorf("memory limit not applied: %+v", res.Memory)
	}
	if res.CPU == nil || res.CPU.Quota == nil || *res.CPU.Quota != 150000 {
		t.Errorf("cpu quota not applied: %+v", res.CPU)
	}
	if res.CPU == nil || res.CPU.Period == nil || *res.CPU.Period != 100000 {
		t.Errorf("cpu period not applied: %+v", res.CPU)
	}
	if res.Pids == nil || res.Pids.Limit != 256 {
		t.Errorf("pids limit not applied: %+v", res.Pids)
	}
}

func TestApplyResourceLimits_NilAndEmpty(t *testing.T) {
	// nil limits: no-op, no panic.
	spec := DefaultSpec("rootfs", []string{"/app"})
	if err := ApplyResourceLimits(spec, nil); err != nil {
		t.Fatalf("nil limits should be a no-op: %v", err)
	}
	if m := spec.Linux.Resources.Memory; m != nil {
		t.Errorf("nil limits should not set memory, got %+v", m)
	}

	// empty limits: leaves every resource unset.
	spec2 := DefaultSpec("rootfs", []string{"/app"})
	if err := ApplyResourceLimits(spec2, &appconfig.ResourceLimits{}); err != nil {
		t.Fatalf("empty limits should be a no-op: %v", err)
	}
	res := spec2.Linux.Resources
	if res.Memory != nil || res.CPU != nil || res.Pids != nil {
		t.Errorf("empty limits should leave resources unset, got %+v", res)
	}
}

func TestApplyResourceLimits_PartialPreservesDeviceRules(t *testing.T) {
	// Setting only memory must not clobber the device cgroup rules DefaultSpec
	// (and entitlements) populate on the same Resources struct.
	spec := DefaultSpec("rootfs", []string{"/app"})
	before := len(spec.Linux.Resources.Devices)
	if err := ApplyResourceLimits(spec, &appconfig.ResourceLimits{Memory: "128Mi"}); err != nil {
		t.Fatalf("ApplyResourceLimits: %v", err)
	}
	if got := len(spec.Linux.Resources.Devices); got != before {
		t.Errorf("device rules changed: before %d, after %d", before, got)
	}
	if spec.Linux.Resources.CPU != nil || spec.Linux.Resources.Pids != nil {
		t.Errorf("only memory should be set")
	}
}

func TestApplyResourceLimits_InvalidValue(t *testing.T) {
	spec := DefaultSpec("rootfs", []string{"/app"})
	err := ApplyResourceLimits(spec, &appconfig.ResourceLimits{Memory: "garbage"})
	if err == nil {
		t.Fatalf("expected error for invalid memory value")
	}
}
