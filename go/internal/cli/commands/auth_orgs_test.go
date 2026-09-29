//go:build darwin || linux || windows

package commands

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	pb "github.com/wendylabsinc/wendy/go/proto/gen/cloudpb/v2"
)

func stubListOrgsV2(t *testing.T, fn func(*config.AuthConfig) ([]*pb.Organization, error)) {
	t.Helper()
	orig := listCloudOrganizationsV2
	listCloudOrganizationsV2 = func(_ context.Context, a *config.AuthConfig) ([]*pb.Organization, error) {
		return fn(a)
	}
	t.Cleanup(func() { listCloudOrganizationsV2 = orig })
}

func v2Session(grpc string) config.AuthConfig {
	return config.AuthConfig{
		CloudGRPC:    grpc,
		Certificates: []config.CertificateInfo{{OrganizationID: 2}},
	}
}

func v2Org(id, name string) *pb.Organization {
	return &pb.Organization{Id: id, Name: name}
}

func tenantSession(grpc, tenant string) config.AuthConfig {
	return config.AuthConfig{
		CloudGRPC:    grpc,
		Certificates: []config.CertificateInfo{{PrincipalURI: "spiffe://wendy.sh/tenant/" + tenant + "/operator/test"}},
	}
}

// TestRemoveCloudSessionCreds covers the management picker's 'r' action: a
// session is keyed by (cloud endpoint, tenant UUID), so removal must match both
// — never the UUID alone, which would drop a same-tenant session on another
// cloud.
func TestRemoveCloudSessionCreds(t *testing.T) {
	const tenantA = "8a53be77-2a69-464f-8f73-83643fe0beaa"
	const tenantB = "11111111-2222-3333-4444-555555555555"

	t.Run("removes the matching session only", func(t *testing.T) {
		cfg := &config.Config{Auth: []config.AuthConfig{
			tenantSession("api.dev.wendy.sh:443", tenantA),
			tenantSession("api.dev.wendy.sh:443", tenantB),
		}}
		if !removeCloudSessionCreds(cfg, "api.dev.wendy.sh:443", tenantA) {
			t.Fatal("want removed=true")
		}
		if len(cfg.Auth) != 1 || cfg.Auth[0].Certificates[0].TenantUUID() != tenantB {
			t.Fatalf("wrong session removed: %+v", cfg.Auth)
		}
	})

	t.Run("no match leaves config untouched", func(t *testing.T) {
		cfg := &config.Config{Auth: []config.AuthConfig{tenantSession("api.dev.wendy.sh:443", tenantA)}}
		if removeCloudSessionCreds(cfg, "api.dev.wendy.sh:443", tenantB) {
			t.Fatal("want removed=false for an unknown tenant")
		}
		if len(cfg.Auth) != 1 {
			t.Fatalf("config mutated on a no-match: %+v", cfg.Auth)
		}
	})

	t.Run("same tenant on a different cloud is kept", func(t *testing.T) {
		cfg := &config.Config{Auth: []config.AuthConfig{
			tenantSession("api.dev.wendy.sh:443", tenantA),
			tenantSession("api.prod.wendy.sh:443", tenantA),
		}}
		if !removeCloudSessionCreds(cfg, "api.dev.wendy.sh:443", tenantA) {
			t.Fatal("want removed=true")
		}
		if len(cfg.Auth) != 1 || cfg.Auth[0].CloudGRPC != "api.prod.wendy.sh:443" {
			t.Fatalf("wrong-cloud session removed: %+v", cfg.Auth)
		}
	})
}

// TestCollectOrgsV2DoesNotSwallowErrors is the regression guard for WDY-3101: a
// failing session must surface its error verbatim and NOT count as an answered
// session. If the old silent `continue` is reintroduced, okSessions would still
// be 0 but the error would be neither recorded nor printed — and this fails.
func TestCollectOrgsV2DoesNotSwallowErrors(t *testing.T) {
	rpcErr := errors.New("rpc error: code = Unimplemented desc = unknown service v1.OrganizationService")
	stubListOrgsV2(t, func(*config.AuthConfig) ([]*pb.Organization, error) { return nil, rpcErr })

	cfg := &config.Config{Auth: []config.AuthConfig{v2Session("api.dev.wendy.sh:443")}}
	var buf bytes.Buffer

	orgs, _, okSessions, failed := collectOrgsV2(context.Background(), cfg, &buf)

	if len(orgs) != 0 {
		t.Fatalf("orgs = %d, want 0 on a failed probe", len(orgs))
	}
	if okSessions != 0 {
		t.Fatalf("okSessions = %d, want 0 — a failed probe must not count as answered", okSessions)
	}
	if len(failed) != 1 {
		t.Fatalf("failed = %d errors, want 1 — the error was swallowed", len(failed))
	}
	if !strings.Contains(buf.String(), "Unimplemented") {
		t.Fatalf("stderr = %q, want the verbatim RPC error surfaced", buf.String())
	}
	// The RunE contract: okSessions == 0 means propagate, never print no-orgs.
	if joined := errors.Join(failed...); joined == nil || !strings.Contains(joined.Error(), "Unimplemented") {
		t.Fatalf("joined error = %v, want the RPC status propagated to the caller", joined)
	}
}

// TestCollectOrgsV2EmptyMembership: a session that answers with an empty list is
// a genuine no-orgs result (okSessions > 0) — the one case where RunE may print
// the no-orgs sentence.
func TestCollectOrgsV2EmptyMembership(t *testing.T) {
	stubListOrgsV2(t, func(*config.AuthConfig) ([]*pb.Organization, error) { return nil, nil })

	cfg := &config.Config{Auth: []config.AuthConfig{v2Session("api.dev.wendy.sh:443")}}
	orgs, _, okSessions, failed := collectOrgsV2(context.Background(), cfg, &bytes.Buffer{})

	if len(orgs) != 0 || okSessions != 1 || len(failed) != 0 {
		t.Fatalf("empty membership: orgs=%d okSessions=%d failed=%d, want 0/1/0", len(orgs), okSessions, len(failed))
	}
}

// TestCollectOrgsV2AggregatesAndDedups: orgs from multiple sessions merge and
// deduplicate by ID, and the first answering session is returned for the picker.
func TestCollectOrgsV2AggregatesAndDedups(t *testing.T) {
	stubListOrgsV2(t, func(a *config.AuthConfig) ([]*pb.Organization, error) {
		switch a.CloudGRPC {
		case "one:443":
			return []*pb.Organization{v2Org("uuid-a", "Acme"), v2Org("uuid-b", "Beta")}, nil
		case "two:443":
			return []*pb.Organization{v2Org("uuid-b", "Beta"), v2Org("uuid-c", "Gamma")}, nil
		}
		return nil, nil
	})

	cfg := &config.Config{Auth: []config.AuthConfig{v2Session("one:443"), v2Session("two:443")}}
	orgs, pickerAuth, okSessions, failed := collectOrgsV2(context.Background(), cfg, &bytes.Buffer{})

	if okSessions != 2 || len(failed) != 0 {
		t.Fatalf("okSessions=%d failed=%d, want 2/0", okSessions, len(failed))
	}
	if len(orgs) != 3 {
		t.Fatalf("orgs = %d, want 3 deduplicated", len(orgs))
	}
	if pickerAuth == nil || pickerAuth.CloudGRPC != "one:443" {
		t.Fatalf("pickerAuth = %v, want the first answering session", pickerAuth)
	}
}

// TestCollectOrgsV2MixedFailureStillSurfaces: one session fails and one answers
// empty. The error is still surfaced verbatim even though another session
// answered, so the failure is never masked by the no-orgs outcome.
func TestCollectOrgsV2MixedFailureStillSurfaces(t *testing.T) {
	stubListOrgsV2(t, func(a *config.AuthConfig) ([]*pb.Organization, error) {
		if a.CloudGRPC == "broken:443" {
			return nil, errors.New("rpc error: code = Unavailable desc = connection refused")
		}
		return nil, nil
	})

	cfg := &config.Config{Auth: []config.AuthConfig{v2Session("broken:443"), v2Session("ok:443")}}
	var buf bytes.Buffer
	orgs, _, okSessions, failed := collectOrgsV2(context.Background(), cfg, &buf)

	if len(orgs) != 0 || okSessions != 1 || len(failed) != 1 {
		t.Fatalf("mixed: orgs=%d okSessions=%d failed=%d, want 0/1/1", len(orgs), okSessions, len(failed))
	}
	if !strings.Contains(buf.String(), "Unavailable") {
		t.Fatalf("stderr = %q, want the failing session's error surfaced", buf.String())
	}
}
