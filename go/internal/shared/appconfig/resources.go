package appconfig

import (
	"fmt"
	"strconv"
	"strings"
)

// cpuCFSPeriod is the CFS scheduler period (in microseconds) used to express a
// fractional-core CPU limit as a quota. 100ms matches the OCI/Docker default,
// so a `cpus` of "1.5" becomes quota=150000 over period=100000.
const cpuCFSPeriod uint64 = 100000

// ResourceLimits declares the resource ceilings the agent enforces on a
// container via cgroups. All fields are optional; an omitted field leaves that
// resource unbounded (the historical behaviour). Limits may be set at the app
// level and overridden per service (see AppConfig.ResolveResourcesForService).
type ResourceLimits struct {
	// Memory is the hard memory limit as a byte count, optionally with a unit
	// suffix: binary (Ki, Mi, Gi, Ti) or decimal (K, M, G, T). A bare number is
	// interpreted as bytes. The container is OOM-killed if it exceeds this.
	Memory string `json:"memory,omitempty"`
	// CPUs is the maximum number of CPU cores, as a decimal (e.g. "0.5", "1.5",
	// "2"). It is enforced as a CFS quota over a fixed 100ms period.
	CPUs string `json:"cpus,omitempty"`
	// PIDs is the maximum number of processes/threads the container may create.
	// A cheap guard against fork bombs. Omitted (0) means unbounded.
	PIDs int64 `json:"pids,omitempty"`
}

// memorySuffixes maps a (lower-cased) unit suffix to its multiplier. The "i"
// variants are binary (powers of 1024); the bare variants are decimal (powers
// of 1000), matching the convention used by Kubernetes resource quantities.
var memorySuffixes = map[string]int64{
	"ki": 1 << 10, "mi": 1 << 20, "gi": 1 << 30, "ti": 1 << 40,
	"k": 1e3, "m": 1e6, "g": 1e9, "t": 1e12,
}

// MemoryLimitBytes parses Memory into a byte count. It returns (nil, nil) when
// Memory is empty (unbounded) and an error when the value is malformed or not a
// positive whole number of bytes.
func (r *ResourceLimits) MemoryLimitBytes() (*int64, error) {
	s := strings.TrimSpace(r.Memory)
	if s == "" {
		return nil, nil
	}

	mult := int64(1)
	// Match the longest unit suffix first so "Mi" is not mistaken for "M".
	lower := strings.ToLower(s)
	for _, suffix := range []string{"ki", "mi", "gi", "ti", "k", "m", "g", "t"} {
		if strings.HasSuffix(lower, suffix) {
			mult = memorySuffixes[suffix]
			s = s[:len(s)-len(suffix)]
			break
		}
	}

	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("memory %q is not a valid byte quantity (e.g. \"512Mi\", \"1Gi\", or a number of bytes)", r.Memory)
	}
	if n <= 0 {
		return nil, fmt.Errorf("memory must be a positive quantity, got %q", r.Memory)
	}
	bytes := n * mult
	return &bytes, nil
}

// CPUQuota parses CPUs into a CFS (quota, period) pair in microseconds. It
// returns (nil, nil, nil) when CPUs is empty (unbounded) and an error when the
// value is malformed or not positive.
func (r *ResourceLimits) CPUQuota() (*int64, *uint64, error) {
	s := strings.TrimSpace(r.CPUs)
	if s == "" {
		return nil, nil, nil
	}
	cores, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("cpus %q is not a valid number of cores (e.g. \"0.5\", \"1\", \"2\")", r.CPUs)
	}
	if cores <= 0 {
		return nil, nil, fmt.Errorf("cpus must be a positive number of cores, got %q", r.CPUs)
	}
	period := cpuCFSPeriod
	quota := int64(cores*float64(period) + 0.5) // round to nearest microsecond
	return &quota, &period, nil
}

// PIDsLimit returns the process-count limit, or nil when unset (unbounded).
func (r *ResourceLimits) PIDsLimit() *int64 {
	if r.PIDs == 0 {
		return nil
	}
	limit := r.PIDs
	return &limit
}

// validate checks that every set field parses and is within range. The prefix
// is used in error messages (e.g. "resources" or "services[\"worker\"].resources").
func (r *ResourceLimits) validate(prefix string) error {
	if r == nil {
		return nil
	}
	if _, err := r.MemoryLimitBytes(); err != nil {
		return fmt.Errorf("%s.%w", prefix, err)
	}
	if _, _, err := r.CPUQuota(); err != nil {
		return fmt.Errorf("%s.%w", prefix, err)
	}
	if r.PIDs < 0 {
		return fmt.Errorf("%s.pids must not be negative, got %d", prefix, r.PIDs)
	}
	return nil
}

// ResolveResourcesForService returns the resource limits that apply to the
// named service. A service that declares its own resources overrides the
// app-level limits wholesale (mirroring ResolveROS2ConfigForService); otherwise
// the app-level Resources are inherited. Returns nil when neither is set.
func (a *AppConfig) ResolveResourcesForService(serviceName string) *ResourceLimits {
	if serviceName != "" {
		if svc, ok := a.Services[serviceName]; ok && svc != nil && svc.Resources != nil {
			return svc.Resources
		}
	}
	return a.Resources
}
