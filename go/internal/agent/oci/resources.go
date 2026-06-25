package oci

import (
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

// ApplyResourceLimits translates the app/service ResourceLimits into cgroup
// constraints on the OCI spec. It is additive: only the fields the developer
// set are applied, and existing Resources entries (notably the device cgroup
// rules established by DefaultSpec and ApplyEntitlements) are preserved. A nil
// or empty limits leaves the spec unchanged. It returns an error if any set
// value fails to parse — the caller should refuse to start the container rather
// than silently run it unbounded.
func ApplyResourceLimits(spec *Spec, limits *appconfig.ResourceLimits) error {
	if limits == nil {
		return nil
	}

	memBytes, err := limits.MemoryLimitBytes()
	if err != nil {
		return err
	}
	quota, period, err := limits.CPUQuota()
	if err != nil {
		return err
	}
	pids := limits.PIDsLimit()

	if memBytes == nil && quota == nil && pids == nil {
		return nil
	}

	if spec.Linux == nil {
		spec.Linux = &Linux{}
	}
	if spec.Linux.Resources == nil {
		spec.Linux.Resources = &LinuxResources{}
	}
	res := spec.Linux.Resources

	if memBytes != nil {
		res.Memory = &LinuxMemory{Limit: memBytes}
	}
	if quota != nil {
		res.CPU = &LinuxCPU{Quota: quota, Period: period}
	}
	if pids != nil {
		res.Pids = &LinuxPids{Limit: *pids}
	}
	return nil
}
