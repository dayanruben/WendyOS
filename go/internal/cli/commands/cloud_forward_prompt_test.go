package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/tui"
)

func TestResolveCloudTunnelArg(t *testing.T) {
	oldInteractive, oldJSON := isInteractiveTerminalFn, jsonOutput
	t.Cleanup(func() { isInteractiveTerminalFn, jsonOutput = oldInteractive, oldJSON })

	for _, tt := range []struct {
		name        string
		args        []string
		interactive bool
		json        bool
		input       string
		want        string
		wantErr     bool
		cancelled   bool
	}{
		{name: "explicit TCP", args: []string{"8080:3000"}, want: "8080:3000"},
		{name: "explicit UDP in JSON", args: []string{"9000/udp"}, json: true, want: "9000/udp"},
		{name: "missing non-interactive", wantErr: true},
		{name: "missing JSON", interactive: true, json: true, wantErr: true},
		{name: "prompt TCP", interactive: true, input: "8080:3000\r", want: "8080:3000"},
		{name: "prompt UDP", interactive: true, input: "9000:9001/udp\r", want: "9000:9001/udp"},
		{name: "prompt single port", interactive: true, input: "8080\r", want: "8080"},
		{name: "retry empty input", interactive: true, input: "\r8080\r", want: "8080"},
		{name: "retry invalid port", interactive: true, input: "65536\r\x158080:3000\r", want: "8080:3000"},
		{name: "retry invalid protocol", interactive: true, input: "8080/sctp\r\x158080/udp\r", want: "8080/udp"},
		{name: "cancel", interactive: true, input: "\x03", cancelled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			isInteractiveTerminalFn = func() bool { return tt.interactive }
			jsonOutput = tt.json
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := newCloudTunnelCmd()
			cmd.SetContext(ctx)
			cmd.SetIn(strings.NewReader(tt.input))
			cmd.SetOut(io.Discard)
			got, err := resolveCloudTunnelArg(cmd, tt.args)
			switch {
			case tt.cancelled:
				if !errors.Is(err, tui.ErrCancelled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			case tt.wantErr:
				if err == nil || !strings.Contains(err.Error(), "provide a port mapping") {
					t.Fatalf("got %v, want explicit mapping guidance", err)
				}
			default:
				if err != nil || got != tt.want {
					t.Fatalf("got (%q, %v), want (%q, nil)", got, err, tt.want)
				}
			}
		})
	}
}

func TestCloudTunnelCommandMissingAndExtraArgs(t *testing.T) {
	stubNonInteractive(t)
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing", args: []string{}, want: "provide a port mapping"},
		{name: "invalid", args: []string{"0"}, want: "invalid port"},
		{name: "extra", args: []string{"8080", "9000"}, want: "accepts at most 1 arg(s)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCloudTunnelCmd()
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}
