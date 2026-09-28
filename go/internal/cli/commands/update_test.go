package commands

import (
	"errors"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

// TestDueCLIUpdateCheckSkipsDevBuilds asserts that development builds — both the
// literal "dev" default and CI branch builds carrying a "-dev" suffix — never
// trigger the periodic CLI update check (WDY-1770).
func TestDueCLIUpdateCheckSkipsDevBuilds(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	for _, ver := range []string{"dev", "2026.06.30-133859-dev"} {
		t.Run(ver, func(t *testing.T) {
			version.Version = ver
			// Empty LastCLIUpdateCheck would otherwise mark the check as due.
			if dueCLIUpdateCheck(&config.Config{}) {
				t.Errorf("dueCLIUpdateCheck for dev build %q = true, want false", ver)
			}
		})
	}
}

// The background update check used to save the config root loaded at startup,
// seconds later — reverting a default device, pin or login the command itself
// had just saved. It must change only its own two fields on the current config.
func TestRecordCLIUpdateCheckKeepsNewerConfig(t *testing.T) {
	setTempConfig(t, &config.Config{})
	if err := config.Save(&config.Config{DefaultDevice: "saved-by-the-command.local"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if err := recordCLIUpdateCheck("", errors.New("offline"), now); err != nil {
		t.Fatalf("recordCLIUpdateCheck: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultDevice != "saved-by-the-command.local" {
		t.Fatalf("the update check reverted DefaultDevice to %q", cfg.DefaultDevice)
	}
	if cfg.LastCLIUpdateCheck != "2026-09-28T12:00:00Z" {
		t.Fatalf("LastCLIUpdateCheck = %q, want 2026-09-28T12:00:00Z", cfg.LastCLIUpdateCheck)
	}
}
