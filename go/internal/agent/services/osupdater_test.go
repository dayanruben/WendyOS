package services

import (
	"context"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/agent/oshealth"
)

// fakeUpdater is a test double for the osUpdater interface.
type fakeUpdater struct {
	nameVal      string
	detectVal    bool
	availableVal bool
}

func (f fakeUpdater) name() string    { return f.nameVal }
func (f fakeUpdater) detect() bool    { return f.detectVal }
func (f fakeUpdater) available() bool { return f.availableVal }
func (f fakeUpdater) install(context.Context, string, func(string, int32)) error {
	return nil
}
func (f fakeUpdater) commit() oshealth.MenderResult   { return oshealth.MenderResult{} }
func (f fakeUpdater) rollback() oshealth.MenderResult { return oshealth.MenderResult{} }

func TestChooseUpdaterForCommit(t *testing.T) {
	// The gate must select the commit backend by binary presence (available),
	// NOT the connector probe (detect): an update was already installed, so a
	// transient detect() failure must not strand a healthy slot uncommitted.
	wendyos := func(detect, available bool) osUpdater {
		return fakeUpdater{nameVal: updaterNameWendyOS, detectVal: detect, availableVal: available}
	}
	mender := func(detect, available bool) osUpdater {
		return fakeUpdater{nameVal: updaterNameMender, detectVal: detect, availableVal: available}
	}

	tests := []struct {
		name       string
		requested  string
		candidates []osUpdater
		wantName   string // "" => expect nil
	}{
		{
			name:       "named wendyos is returned even when detect fails (binary present)",
			requested:  updaterNameWendyOS,
			candidates: []osUpdater{wendyos(false, true), mender(true, true)},
			wantName:   updaterNameWendyOS,
		},
		{
			name:       "named mender is returned",
			requested:  updaterNameMender,
			candidates: []osUpdater{wendyos(true, true), mender(false, true)},
			wantName:   updaterNameMender,
		},
		{
			name:       "auto prefers an available wendyos without probing detect",
			requested:  "",
			candidates: []osUpdater{wendyos(false, true), mender(false, true)},
			wantName:   updaterNameWendyOS,
		},
		{
			name:       "auto falls back to mender when wendyos binary is absent",
			requested:  "auto",
			candidates: []osUpdater{wendyos(false, false), mender(false, true)},
			wantName:   updaterNameMender,
		},
		{
			name:       "auto returns nil when no backend binary is present",
			requested:  "auto",
			candidates: []osUpdater{wendyos(false, false), mender(false, false)},
			wantName:   "",
		},
		{
			name:       "unknown backend returns nil",
			requested:  "bogus",
			candidates: []osUpdater{wendyos(true, true), mender(true, true)},
			wantName:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chooseUpdaterForCommit(tt.requested, tt.candidates)
			if tt.wantName == "" {
				if got != nil {
					t.Fatalf("chooseUpdaterForCommit(%q) = %q, want nil", tt.requested, got.name())
				}
				return
			}
			if got == nil {
				t.Fatalf("chooseUpdaterForCommit(%q) = nil, want %q", tt.requested, tt.wantName)
			}
			if got.name() != tt.wantName {
				t.Fatalf("chooseUpdaterForCommit(%q) = %q, want %q", tt.requested, got.name(), tt.wantName)
			}
		})
	}
}

func TestRequestedBackendFromMarker(t *testing.T) {
	tests := []struct {
		name   string
		marker oshealth.PendingMarker
		found  bool
		want   string
	}{
		{name: "no marker selects auto", found: false, want: ""},
		{name: "legacy marker without backend selects auto", marker: oshealth.PendingMarker{}, found: true, want: ""},
		{name: "marker backend is honored", marker: oshealth.PendingMarker{Backend: updaterNameWendyOS}, found: true, want: updaterNameWendyOS},
		{name: "mender marker is honored", marker: oshealth.PendingMarker{Backend: updaterNameMender}, found: true, want: updaterNameMender},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestedBackendFromMarker(tt.marker, tt.found); got != tt.want {
				t.Fatalf("requestedBackendFromMarker = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChooseUpdater(t *testing.T) {
	wendyosUp := func(detect bool) osUpdater { return fakeUpdater{nameVal: updaterNameWendyOS, detectVal: detect} }
	menderUp := func(detect bool) osUpdater { return fakeUpdater{nameVal: updaterNameMender, detectVal: detect} }

	tests := []struct {
		name       string
		requested  string
		candidates []osUpdater
		wantName   string
		wantErr    bool
	}{
		{
			name:       "auto prefers wendyos when it detects",
			requested:  "auto",
			candidates: []osUpdater{wendyosUp(true), menderUp(true)},
			wantName:   updaterNameWendyOS,
		},
		{
			name:       "auto falls back to mender when wendyos does not detect",
			requested:  "auto",
			candidates: []osUpdater{wendyosUp(false), menderUp(true)},
			wantName:   updaterNameMender,
		},
		{
			name:       "empty string behaves like auto",
			requested:  "",
			candidates: []osUpdater{wendyosUp(false), menderUp(true)},
			wantName:   updaterNameMender,
		},
		{
			name:       "auto errors when nothing detects",
			requested:  "auto",
			candidates: []osUpdater{wendyosUp(false), menderUp(false)},
			wantErr:    true,
		},
		{
			name:       "explicit mender is honored even when wendyos detects",
			requested:  "mender",
			candidates: []osUpdater{wendyosUp(true), menderUp(true)},
			wantName:   updaterNameMender,
		},
		{
			name:       "explicit wendyos is honored even when mender detects",
			requested:  "wendyos",
			candidates: []osUpdater{wendyosUp(true), menderUp(true)},
			wantName:   updaterNameWendyOS,
		},
		{
			name:       "wendyos-update alias selects wendyos",
			requested:  "wendyos-update",
			candidates: []osUpdater{wendyosUp(true), menderUp(true)},
			wantName:   updaterNameWendyOS,
		},
		{
			name:       "explicit mender errors when mender unavailable (no silent fallback)",
			requested:  "mender",
			candidates: []osUpdater{wendyosUp(true), menderUp(false)},
			wantErr:    true,
		},
		{
			name:       "explicit wendyos errors when wendyos undetected (no silent fallback)",
			requested:  "wendyos",
			candidates: []osUpdater{wendyosUp(false), menderUp(true)},
			wantErr:    true,
		},
		{
			name:       "unknown backend value errors",
			requested:  "bogus",
			candidates: []osUpdater{wendyosUp(true), menderUp(true)},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chooseUpdater(tt.requested, tt.candidates)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("chooseUpdater(%q) = %v, want error", tt.requested, got.name())
				}
				return
			}
			if err != nil {
				t.Fatalf("chooseUpdater(%q) returned error: %v", tt.requested, err)
			}
			if got.name() != tt.wantName {
				t.Fatalf("chooseUpdater(%q) = %q, want %q", tt.requested, got.name(), tt.wantName)
			}
		})
	}
}
