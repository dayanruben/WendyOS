package commands

import (
	"io"
	"strings"
	"testing"
)

// TestAuthLoginModeSelection covers the login mode switch (WDY-3163): the
// default is new-cloud OIDC, the old dashboard flow is only behind --legacy,
// and every guarded combination errors before any network call. Cases here
// deliberately avoid the paths that dial wendy-auth/pki-core.
func TestAuthLoginModeSelection(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "bare login no longer falls back to prod",
			args:    nil,
			wantErr: "provide --email",
		},
		{
			name:    "legacy conflicts with email",
			args:    []string{"--legacy", "--email", "a@b.com"},
			wantErr: "--legacy selects the old cloud-dashboard login",
		},
		{
			name:    "legacy conflicts with issuer",
			args:    []string{"--legacy", "--issuer", "https://auth.example/realms/x"},
			wantErr: "--legacy selects the old cloud-dashboard login",
		},
		{
			name:    "api-key conflicts with oidc",
			args:    []string{"--api-key", "wnd_x", "--email", "a@b.com"},
			wantErr: "select different login modes",
		},
		{
			name:    "api-key needs cloud-grpc",
			args:    []string{"--api-key", "wnd_x"},
			wantErr: "--cloud-grpc is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newAuthLoginCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("args %v: got err %v, want containing %q", tc.args, err, tc.wantErr)
			}
		})
	}
}

func TestAuthLoginLegacyFlagDefaultsOff(t *testing.T) {
	flag := newAuthLoginCmd().Flags().Lookup("legacy")
	if flag == nil {
		t.Fatal("--legacy flag is missing")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--legacy default = %q, want false", flag.DefValue)
	}
}
