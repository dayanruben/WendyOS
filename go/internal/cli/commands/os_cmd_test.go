package commands

import (
	"testing"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

func TestOSAlreadyCurrent(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		nightly bool
		want    bool
	}{
		{"stable equal is current", "WendyOS-0.10.4", "0.10.4", false, true},
		{"stable newer available", "WendyOS-0.10.4", "0.12.0", false, false},
		{"stable device ahead is current", "WendyOS-0.12.0", "0.10.4", false, true},
		{"nightly equal is current", "WendyOS-0.12.0-nightly", "0.12.0-nightly", true, true},
		{"nightly different available", "WendyOS-0.12.0-nightly", "0.13.0-nightly", true, false},
		{"empty current not current", "", "0.10.4", false, false},
		{"empty latest not current", "WendyOS-0.10.4", "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := osAlreadyCurrent(tc.current, tc.latest, tc.nightly); got != tc.want {
				t.Fatalf("osAlreadyCurrent(%q,%q,%v) = %v, want %v", tc.current, tc.latest, tc.nightly, got, tc.want)
			}
		})
	}
}

func TestDecideOSUpdate(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		latest      string
		nightly     bool
		assumeYes   bool
		interactive bool
		want        osUpdateAction
	}{
		{"already current", "WendyOS-0.10.4", "0.10.4", false, false, false, osActionAlreadyCurrent},
		{"newer with yes", "WendyOS-0.10.4", "0.12.0", false, true, false, osActionApply},
		{"newer with yes overrides tty", "WendyOS-0.10.4", "0.12.0", false, true, true, osActionApply},
		{"newer interactive prompts", "WendyOS-0.10.4", "0.12.0", false, false, true, osActionPrompt},
		{"newer noninteractive reports", "WendyOS-0.10.4", "0.12.0", false, false, false, osActionReportOnly},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decideOSUpdate(tc.current, tc.latest, tc.nightly, tc.assumeYes, tc.interactive)
			if got != tc.want {
				t.Fatalf("decideOSUpdate(%q,%q,nightly=%v,yes=%v,tty=%v) = %v, want %v",
					tc.current, tc.latest, tc.nightly, tc.assumeYes, tc.interactive, got, tc.want)
			}
		})
	}
}

func TestValidateOSUpdateIdentityAllowsWendyOSBeforeMenderCheck(t *testing.T) {
	osVersion := "WendyOS-0.10.4"
	resp := &agentpb.GetAgentVersionResponse{Os: "linux", OsVersion: &osVersion}
	if err := validateOSUpdateIdentity(resp); err != nil {
		t.Fatalf("validateOSUpdateIdentity() error = %v, want nil", err)
	}
}

func TestValidateOSUpdateTarget(t *testing.T) {
	strp := func(s string) *string { return &s }

	tests := []struct {
		name string
		resp *agentpb.GetAgentVersionResponse
		want string
	}{
		{
			name: "generic setup is not compatible",
			resp: &agentpb.GetAgentVersionResponse{Os: "darwin"},
			want: osUpdateUnsupportedMessage,
		},
		{
			name: "macOS version does not imply WendyOS",
			resp: &agentpb.GetAgentVersionResponse{Os: "darwin", OsVersion: strp("14.4.1")},
			want: osUpdateUnsupportedMessage,
		},
		{
			name: "linux host with agent is not WendyOS",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux"},
			want: linuxOSUpdateUnsupportedMessage,
		},
		{
			name: "linux host with mender is still not WendyOS",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux", Featureset: []string{"mender"}},
			want: linuxOSUpdateUnsupportedMessage,
		},
		{
			name: "linux OS version does not imply WendyOS",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux", OsVersion: strp("22.04")},
			want: linuxOSUpdateUnsupportedMessage,
		},
		{
			name: "WendyOS without mender is unsupported",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux", OsVersion: strp("WendyOS-0.10.4")},
			want: wendyOSMissingMenderMessage,
		},
		{
			name: "WendyOS version with mender is supported",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux", OsVersion: strp("WendyOS-0.10.4"), Featureset: []string{"mender"}},
		},
		{
			name: "WendyOS device type with mender is supported",
			resp: &agentpb.GetAgentVersionResponse{Os: "linux", DeviceType: strp("raspberry-pi-5"), Featureset: []string{"mender"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOSUpdateTarget(tc.resp)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validateOSUpdateTarget() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateOSUpdateTarget() error = nil, want %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Fatalf("validateOSUpdateTarget() error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestProgressLabel(t *testing.T) {
	tests := []struct {
		phase   string
		percent int32
		want    string
	}{
		{"installing", 42, "Installing update (42%)"},
		{"installing", 0, "Installing update..."},
		{"downloading", 0, "Downloading update..."},
		{"finalizing", 100, "Finalizing (100%)"},
		{"", 0, "Updating WendyOS..."},
	}
	for _, tc := range tests {
		if got := progressLabel(tc.phase, tc.percent); got != tc.want {
			t.Errorf("progressLabel(%q,%d) = %q, want %q", tc.phase, tc.percent, got, tc.want)
		}
	}
}
