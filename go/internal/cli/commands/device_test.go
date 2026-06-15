package commands

import (
	"context"
	"testing"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

func TestDeviceCmd_HasPs(t *testing.T) {
	cmd := newDeviceCmd()
	var found bool
	for _, sub := range cmd.Commands() {
		if sub.Use == "ps" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'ps' subcommand on device command")
	}
}

func TestDefaultEnrollmentName(t *testing.T) {
	cases := map[string]string{
		"playful-reed.local": "playful-reed",
		"playful-reed":       "playful-reed",
		"192.168.1.50":       "",
		"":                   "",
	}
	for in, want := range cases {
		if got := defaultEnrollmentName(in); got != want {
			t.Errorf("defaultEnrollmentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaybeCheckOSUpdateSkips(t *testing.T) {
	strp := func(s string) *string { return &s }

	tests := []struct {
		name    string
		version *agentpb.GetAgentVersionResponse
	}{
		{"nil version", nil},
		{"non-wendyos darwin", &agentpb.GetAgentVersionResponse{Os: "darwin", OsVersion: strp("14.4")}},
		{"wendyos without mender",
			&agentpb.GetAgentVersionResponse{Os: "linux", OsVersion: strp("WendyOS-0.10.4"), DeviceType: strp("raspberry-pi-5")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// These inputs must return from the cheap pre-reconnect gate, before
			// any reconnect/manifest/network call. (WendyOS+mender devices do
			// reconnect to re-read the version, so they're not covered here.)
			// A nil connection is safe because the gate returns before it is used.
			if err := maybeCheckOSUpdate(context.Background(), tc.version, nil, false, false, ""); err != nil {
				t.Fatalf("maybeCheckOSUpdate() error = %v, want nil", err)
			}
		})
	}
}
