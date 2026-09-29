//go:build darwin || linux || windows

package commands

import (
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

func selectorConfig() *config.Config {
	return &config.Config{Auth: []config.AuthConfig{
		{CloudDashboard: "https://cloud.wendy.dev", CloudGRPC: "prod.example.com:443", Certificates: []config.CertificateInfo{{OrganizationID: 7}}},
		{CloudDashboard: "http://localhost:3000", CloudGRPC: "localhost:50051", Certificates: []config.CertificateInfo{{OrganizationID: 1}}},
	}}
}

func TestMatchAuthSelectorByOrgID(t *testing.T) {
	a, err := matchAuthSelector(selectorConfig(), "7")
	if err != nil || a.CloudGRPC != "prod.example.com:443" {
		t.Fatalf("org match failed: %v / %v", a, err)
	}
}

func TestMatchAuthSelectorByEndpointSubstring(t *testing.T) {
	a, err := matchAuthSelector(selectorConfig(), "localhost")
	if err != nil || a.CloudGRPC != "localhost:50051" {
		t.Fatalf("substring match failed: %v / %v", a, err)
	}
}

func TestMatchAuthSelectorNoMatch(t *testing.T) {
	if _, err := matchAuthSelector(selectorConfig(), "nope"); err == nil || !strings.Contains(err.Error(), "no auth session matches") {
		t.Fatalf("want no-match error, got %v", err)
	}
}

// An org ID the user is not logged into is the common failure (the org exists
// in the cloud, just not on this machine), so the error must not read as
// "no such org" — it names the stored sessions and the command that fixes it.
func TestMatchAuthSelectorUnknownOrgIDExplainsLogin(t *testing.T) {
	_, err := matchAuthSelector(selectorConfig(), "75")
	if err == nil {
		t.Fatal("want error for an org with no stored session")
	}
	got := err.Error()
	for _, want := range []string{
		"not logged in to org 75",
		"wendy auth login",
		"org 7 — prod.example.com:443",
		"org 1 — localhost:50051",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("error missing %q:\n%s", want, got)
		}
	}
	// It must not claim the org does not exist: this command never asks the cloud.
	if strings.Contains(got, "does not exist") {
		t.Errorf("error should not assert the org is missing:\n%s", got)
	}
}

// A non-numeric selector is an endpoint substring, so the wording stays as a
// selector miss rather than a login problem — but it still lists what exists.
func TestMatchAuthSelectorUnknownSubstringListsSessions(t *testing.T) {
	_, err := matchAuthSelector(selectorConfig(), "nope")
	if err == nil {
		t.Fatal("want error for an unmatched substring")
	}
	got := err.Error()
	if !strings.Contains(got, `no auth session matches "nope"`) {
		t.Errorf("want selector-miss wording, got:\n%s", got)
	}
	if !strings.Contains(got, "prod.example.com:443") {
		t.Errorf("error should list stored sessions, got:\n%s", got)
	}
}

func TestMatchAuthSelectorAmbiguous(t *testing.T) {
	cfg := selectorConfig()
	cfg.Auth[1].Certificates[0].OrganizationID = 7 // two sessions in org 7
	_, err := matchAuthSelector(cfg, "7")
	if err == nil || !strings.Contains(err.Error(), "matches multiple sessions") {
		t.Fatalf("want ambiguous error, got %v", err)
	}
	// The error lists each candidate so the user can disambiguate.
	if !strings.Contains(err.Error(), "prod.example.com:443") || !strings.Contains(err.Error(), "localhost:50051") {
		t.Fatalf("ambiguous error should list candidates, got %v", err)
	}
}

// seedConfig writes cfg as the CLI config in a temp HOME and returns a loader
// for asserting what a command persisted.
func seedConfig(t *testing.T, cfg *config.Config) func() *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(cfg); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	return func() *config.Config {
		c, err := config.Load()
		if err != nil {
			t.Fatalf("reloading config: %v", err)
		}
		return c
	}
}

// sharedEndpointConfig models several orgs on one cloud endpoint (login order:
// org 9 first) — the shape where an endpoint-only default is ambiguous.
func sharedEndpointConfig() *config.Config {
	return &config.Config{Auth: []config.AuthConfig{
		{CloudDashboard: "https://cloud.wendy.dev", CloudGRPC: "prod:443", Certificates: []config.CertificateInfo{{OrganizationID: 9}}},
		{CloudDashboard: "https://cloud.wendy.dev", CloudGRPC: "prod:443", Certificates: []config.CertificateInfo{{OrganizationID: 75}}},
	}}
}

// `wendy auth use 75` (legacy org-id selector) selects org 75's context on a
// shared endpoint and makes it current; resolution then returns that session,
// not the first login (org 9).
func TestAuthUsePersistsOrgDefault(t *testing.T) {
	load := seedConfig(t, sharedEndpointConfig())

	cmd := newAuthUseCmd()
	cmd.SetArgs([]string{"75"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth use 75: %v", err)
	}

	cfg := load()
	if cfg.CurrentContext != "org-75" {
		t.Errorf("CurrentContext = %q, want org-75", cfg.CurrentContext)
	}
	auth, err := config.ResolveAuth(cfg, "", nil)
	if err != nil {
		t.Fatalf("ResolveAuth: %v", err)
	}
	if got := auth.Certificates[0].OrganizationID; got != 75 {
		t.Fatalf("resolution after 'auth use 75' returned org %d, want 75", got)
	}
}

// `wendy auth use <name>` selects a context by its name.
func TestAuthUseByContextName(t *testing.T) {
	load := seedConfig(t, sharedEndpointConfig())

	cmd := newAuthUseCmd()
	cmd.SetArgs([]string{"org-75"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth use org-75: %v", err)
	}
	if cfg := load(); cfg.CurrentContext != "org-75" {
		t.Fatalf("CurrentContext = %q, want org-75", cfg.CurrentContext)
	}
}

func TestAuthDefaultClearClearsCurrentContext(t *testing.T) {
	seeded := sharedEndpointConfig()
	seeded.CurrentContext = "org-75"
	load := seedConfig(t, seeded)

	cmd := newAuthDefaultCmd()
	cmd.SetArgs([]string{"--clear"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth default --clear: %v", err)
	}

	if cfg := load(); cfg.CurrentContext != "" {
		t.Fatalf("clear must reset the current context, got %q", cfg.CurrentContext)
	}
}

// The session picker's 'd' key goes through persistSessionDefault; it must set
// the current context to the operator-preferred entry for the selected key.
func TestPersistSessionDefaultSetsContext(t *testing.T) {
	load := seedConfig(t, sharedEndpointConfig())

	if err := persistSessionDefault("prod:443::75"); err != nil {
		t.Fatalf("persistSessionDefault: %v", err)
	}
	if cfg := load(); cfg.CurrentContext != "org-75" {
		t.Fatalf("want current context org-75, got %q", cfg.CurrentContext)
	}

	// A key with no matching session is a bug in the caller, not a silent no-op.
	if err := persistSessionDefault("dev:50051"); err == nil {
		t.Fatal("persistSessionDefault should error on an unknown key")
	}
}
