package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/providers"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

// `wendy discover --json` used to leave a slow Docker out of the list without
// a word. It is still left out (discovery must stay fast), but reported.
func TestDiscoverExternalDevicesReporting_ReportsRuntimesThatDidNotAnswer(t *testing.T) {
	slow := &fakeProvider{key: providers.ProviderKeyDocker, discoverErr: &providers.ProbeTimeoutError{Runtime: "Docker", After: 3 * time.Second}}
	broken := &fakeProvider{key: "wendy-lite", discoverErr: errors.New("serial port busy")}
	ok := &fakeProvider{key: "other", devices: []models.ExternalDevice{extDevice("board")}}
	withExternalProviders(t, slow, broken, ok)

	var skipped []error
	got := discoverExternalDevicesReporting(context.Background(), true, func(err error) { skipped = append(skipped, err) })
	if len(got) != 1 || got[0].DisplayName != "board" {
		t.Fatalf("devices = %v, want only the responsive provider's device", got)
	}
	if len(skipped) != 1 || skipped[0].Error() != "Docker did not answer within 3s" {
		t.Fatalf("skipped = %v, want only the timed-out runtime reported", skipped)
	}
}

// probeRecordingProvider records the probe bound DiscoverDevices was given.
type probeRecordingProvider struct {
	*fakeProvider
	gotTimeout time.Duration
}

func (p *probeRecordingProvider) DiscoverDevices(ctx context.Context) ([]models.ExternalDevice, error) {
	p.gotTimeout = providers.ProbeTimeout(ctx)
	return p.fakeProvider.DiscoverDevices(ctx)
}

// `--device docker` against a slow-but-healthy daemon used to fail with "no
// Docker devices found" after the 3 s discovery bound.
func TestExplicitProviderDevice_LongerBoundAndDistinctTimeoutError(t *testing.T) {
	p := &probeRecordingProvider{fakeProvider: &fakeProvider{key: providers.ProviderKeyDocker,
		discoverErr: &providers.ProbeTimeoutError{Runtime: "Docker", After: explicitProviderProbeTimeout}}}

	_, err := explicitProviderDevice(context.Background(), p)
	if p.gotTimeout != explicitProviderProbeTimeout || explicitProviderProbeTimeout < 10*time.Second {
		t.Fatalf("probe bound = %s, want explicitProviderProbeTimeout (>= 10s)", p.gotTimeout)
	}
	if err == nil || !strings.Contains(err.Error(), "did not answer within 10s") || strings.Contains(err.Error(), "no Docker devices") {
		t.Fatalf("err = %v, want the timeout named, not \"no devices found\"", err)
	}

	p.discoverErr = nil
	p.devices = []models.ExternalDevice{extDevice("docker")}
	sel, err := explicitProviderDevice(context.Background(), p)
	if err != nil || sel == nil || sel.External == nil || sel.Provider != p {
		t.Fatalf("explicitProviderDevice = %+v, %v; want the provider's device selected", sel, err)
	}
}
