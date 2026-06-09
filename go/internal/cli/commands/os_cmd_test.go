package commands

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

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

func TestEvaluateOSUpdateOutcome(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Minute).Unix()
	stale := now.Add(-2 * time.Hour).Unix()

	committed := &agentpb.GetOSUpdateStatusResponse{
		HasResult:     true,
		Outcome:       agentpb.GetOSUpdateStatusResponse_OUTCOME_COMMITTED,
		NewOsVersion:  "WendyOS-0.11.0",
		CreatedAtUnix: fresh,
		Services: []*agentpb.GetOSUpdateStatusResponse_ServiceResult{
			{Unit: "avahi-daemon.service", Status: agentpb.GetOSUpdateStatusResponse_ServiceResult_STATUS_HEALTHY},
		},
	}
	rolledBack := &agentpb.GetOSUpdateStatusResponse{
		HasResult:     true,
		Outcome:       agentpb.GetOSUpdateStatusResponse_OUTCOME_ROLLED_BACK,
		OldOsVersion:  "WendyOS-0.10.4",
		NewOsVersion:  "WendyOS-0.11.0",
		CreatedAtUnix: fresh,
		Services: []*agentpb.GetOSUpdateStatusResponse_ServiceResult{
			{Unit: "avahi-daemon.service", Status: agentpb.GetOSUpdateStatusResponse_ServiceResult_STATUS_FAILED, Reason: "timed out after 30s waiting for active"},
			{Unit: "containerd.service", Status: agentpb.GetOSUpdateStatusResponse_ServiceResult_STATUS_HEALTHY},
		},
	}
	rollbackFailed := &agentpb.GetOSUpdateStatusResponse{
		HasResult:     true,
		Outcome:       agentpb.GetOSUpdateStatusResponse_OUTCOME_ROLLBACK_FAILED,
		CreatedAtUnix: fresh,
		RollbackError: "mender-update reported nothing to roll back",
		Services: []*agentpb.GetOSUpdateStatusResponse_ServiceResult{
			{Unit: "avahi-daemon.service", Status: agentpb.GetOSUpdateStatusResponse_ServiceResult_STATUS_FAILED, Reason: "timed out"},
		},
	}
	commitFailed := &agentpb.GetOSUpdateStatusResponse{
		HasResult:     true,
		Outcome:       agentpb.GetOSUpdateStatusResponse_OUTCOME_COMMIT_FAILED,
		CreatedAtUnix: fresh,
	}

	tests := []struct {
		name         string
		resp         *agentpb.GetOSUpdateStatusResponse
		rpcErr       error
		preVer       string
		postVer      string
		wantErr      bool
		wantContains []string
	}{
		{
			name:         "committed is verified success",
			resp:         committed,
			preVer:       "WendyOS-0.10.4",
			postVer:      "WendyOS-0.11.0",
			wantErr:      false,
			wantContains: []string{"verified"},
		},
		{
			name:    "committed for a version the device is not running is rejected",
			resp:    committed,
			preVer:  "WendyOS-0.10.4",
			postVer: "WendyOS-0.10.4",
			wantErr: true,
			wantContains: []string{
				"WendyOS-0.11.0",
				"WendyOS-0.10.4",
			},
		},
		{
			name:         "committed with unknown running version is trusted",
			resp:         committed,
			preVer:       "WendyOS-0.10.4",
			postVer:      "",
			wantErr:      false,
			wantContains: []string{"verified", "WendyOS-0.11.0"},
		},
		{
			name:    "rolled back reports failed services",
			resp:    rolledBack,
			preVer:  "WendyOS-0.10.4",
			postVer: "WendyOS-0.10.4",
			wantErr: true,
			wantContains: []string{
				"rolled back",
				"avahi-daemon.service",
				"timed out after 30s",
				"WendyOS-0.10.4",
			},
		},
		{
			name:         "rollback failed reports degraded state",
			resp:         rollbackFailed,
			preVer:       "WendyOS-0.10.4",
			postVer:      "WendyOS-0.11.0",
			wantErr:      true,
			wantContains: []string{"avahi-daemon.service", "nothing to roll back"},
		},
		{
			name:         "commit failed is an error",
			resp:         commitFailed,
			preVer:       "WendyOS-0.10.4",
			postVer:      "WendyOS-0.11.0",
			wantErr:      true,
			wantContains: []string{"commit"},
		},
		{
			name:         "unimplemented with unchanged version warns of rollback",
			rpcErr:       status.Error(codes.Unimplemented, "unknown method"),
			preVer:       "WendyOS-0.10.4",
			postVer:      "WendyOS-0.10.4",
			wantErr:      true,
			wantContains: []string{"WendyOS-0.10.4"},
		},
		{
			name:         "unimplemented with changed version succeeds without verification",
			rpcErr:       status.Error(codes.Unimplemented, "unknown method"),
			preVer:       "WendyOS-0.10.4",
			postVer:      "WendyOS-0.11.0",
			wantErr:      false,
			wantContains: []string{"WendyOS-0.11.0"},
		},
		{
			name:    "no record with changed version succeeds without verification",
			resp:    &agentpb.GetOSUpdateStatusResponse{HasResult: false},
			preVer:  "WendyOS-0.10.4",
			postVer: "WendyOS-0.11.0",
			wantErr: false,
		},
		{
			name: "stale record falls back to version comparison",
			resp: &agentpb.GetOSUpdateStatusResponse{
				HasResult:     true,
				Outcome:       agentpb.GetOSUpdateStatusResponse_OUTCOME_COMMITTED,
				CreatedAtUnix: stale,
			},
			preVer:  "WendyOS-0.10.4",
			postVer: "WendyOS-0.10.4",
			wantErr: true,
		},
		{
			name:         "unknown post version cannot verify but does not fail",
			resp:         &agentpb.GetOSUpdateStatusResponse{HasResult: false},
			preVer:       "WendyOS-0.10.4",
			postVer:      "",
			wantErr:      false,
			wantContains: []string{"could not be verified"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := evaluateOSUpdateOutcome(tc.resp, tc.rpcErr, tc.preVer, tc.postVer, now)
			if tc.wantErr && err == nil {
				t.Fatalf("error = nil, want non-nil; msg = %q", msg)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("error = %v, want nil; msg = %q", err, msg)
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q missing %q", msg, want)
				}
			}
		})
	}
}
