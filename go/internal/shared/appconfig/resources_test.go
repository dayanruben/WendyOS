package appconfig

import "testing"

func TestLoadFromBytes_Resources(t *testing.T) {
	data := []byte(`{
		"appId": "demo",
		"resources": { "memory": "512Mi", "cpus": "1.5", "pids": 256 },
		"services": {
			"worker": { "context": "./worker", "resources": { "memory": "256Mi" } }
		}
	}`)
	cfg, err := LoadFromBytes(data)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	if cfg.Resources == nil || cfg.Resources.Memory != "512Mi" || cfg.Resources.CPUs != "1.5" || cfg.Resources.PIDs != 256 {
		t.Fatalf("app-level resources not decoded: %+v", cfg.Resources)
	}
	svc := cfg.Services["worker"]
	if svc == nil || svc.Resources == nil || svc.Resources.Memory != "256Mi" {
		t.Fatalf("service-level resources not decoded: %+v", svc)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestResourceLimits_MemoryLimitBytes(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantNil bool
		wantErr bool
	}{
		{in: "", wantNil: true},
		{in: "1048576", want: 1048576},
		{in: "512Mi", want: 512 * 1024 * 1024},
		{in: "1Gi", want: 1024 * 1024 * 1024},
		{in: "256Ki", want: 256 * 1024},
		{in: "1M", want: 1000 * 1000},
		{in: "2G", want: 2 * 1000 * 1000 * 1000},
		{in: "512mi", want: 512 * 1024 * 1024}, // case-insensitive suffix
		{in: "0", wantErr: true},               // zero is not a useful limit
		{in: "-1", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "12Xi", wantErr: true},
		{in: "1.5Gi", wantErr: true}, // fractional bytes not allowed
	}
	for _, c := range cases {
		r := &ResourceLimits{Memory: c.in}
		got, err := r.MemoryLimitBytes()
		if c.wantErr {
			if err == nil {
				t.Errorf("Memory %q: expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Memory %q: unexpected error %v", c.in, err)
			continue
		}
		if c.wantNil {
			if got != nil {
				t.Errorf("Memory %q: expected nil, got %d", c.in, *got)
			}
			continue
		}
		if got == nil || *got != c.want {
			t.Errorf("Memory %q: want %d, got %v", c.in, c.want, got)
		}
	}
}

func TestResourceLimits_CPUQuota(t *testing.T) {
	cases := []struct {
		in         string
		wantQuota  int64
		wantPeriod uint64
		wantNil    bool
		wantErr    bool
	}{
		{in: "", wantNil: true},
		{in: "1", wantQuota: 100000, wantPeriod: 100000},
		{in: "1.5", wantQuota: 150000, wantPeriod: 100000},
		{in: "0.5", wantQuota: 50000, wantPeriod: 100000},
		{in: "2", wantQuota: 200000, wantPeriod: 100000},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "abc", wantErr: true},
	}
	for _, c := range cases {
		r := &ResourceLimits{CPUs: c.in}
		quota, period, err := r.CPUQuota()
		if c.wantErr {
			if err == nil {
				t.Errorf("CPUs %q: expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("CPUs %q: unexpected error %v", c.in, err)
			continue
		}
		if c.wantNil {
			if quota != nil || period != nil {
				t.Errorf("CPUs %q: expected nil quota/period", c.in)
			}
			continue
		}
		if quota == nil || *quota != c.wantQuota {
			t.Errorf("CPUs %q: want quota %d, got %v", c.in, c.wantQuota, quota)
		}
		if period == nil || *period != c.wantPeriod {
			t.Errorf("CPUs %q: want period %d, got %v", c.in, c.wantPeriod, period)
		}
	}
}

func TestResourceLimits_PIDsLimit(t *testing.T) {
	if got := (&ResourceLimits{}).PIDsLimit(); got != nil {
		t.Errorf("zero PIDs: want nil, got %d", *got)
	}
	if got := (&ResourceLimits{PIDs: 256}).PIDsLimit(); got == nil || *got != 256 {
		t.Errorf("PIDs 256: want 256, got %v", got)
	}
}

func TestValidate_Resources(t *testing.T) {
	cases := []struct {
		name    string
		res     *ResourceLimits
		wantErr bool
	}{
		{name: "nil resources ok", res: nil},
		{name: "valid all fields", res: &ResourceLimits{Memory: "512Mi", CPUs: "1.5", PIDs: 256}},
		{name: "bad memory", res: &ResourceLimits{Memory: "lots"}, wantErr: true},
		{name: "bad cpus", res: &ResourceLimits{CPUs: "-2"}, wantErr: true},
		{name: "negative pids", res: &ResourceLimits{PIDs: -5}, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &AppConfig{AppID: "demo", Resources: c.res}
			err := cfg.Validate()
			if c.wantErr && err == nil {
				t.Errorf("expected validation error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidate_ServiceResources(t *testing.T) {
	cfg := &AppConfig{
		AppID: "demo",
		Services: map[string]*ServiceConfig{
			"worker": {Context: "./worker", Resources: &ResourceLimits{Memory: "nonsense"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for invalid service resources")
	}
}

func TestResolveResourcesForService(t *testing.T) {
	group := &ResourceLimits{Memory: "1Gi"}
	svc := &ResourceLimits{Memory: "256Mi"}
	cfg := &AppConfig{
		Resources: group,
		Services: map[string]*ServiceConfig{
			"worker": {Context: "./worker", Resources: svc},
			"web":    {Context: "./web"},
		},
	}
	if got := cfg.ResolveResourcesForService("worker"); got != svc {
		t.Errorf("worker should use service-level resources")
	}
	if got := cfg.ResolveResourcesForService("web"); got != group {
		t.Errorf("web should inherit app-level resources")
	}
	if got := cfg.ResolveResourcesForService(""); got != group {
		t.Errorf("single-container app should use app-level resources")
	}
	if got := (&AppConfig{}).ResolveResourcesForService("x"); got != nil {
		t.Errorf("absent resources should resolve to nil")
	}
}
