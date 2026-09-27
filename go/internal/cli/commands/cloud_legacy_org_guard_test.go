package commands

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A cloud (OIDC) session carries a SPIFFE principal and no legacy int32 org id,
// so the legacy v1 asset listing would query organization 0 and hand back an
// empty page with no error. That silent-empty is WDY-3059: it read to the
// operator as "you have no devices" rather than "this command cannot see your
// devices". The guard must refuse instead, and say so in terms that name the
// commands that do work.
func TestFetchCloudAssetsFilteredRefusesPrincipalSession(t *testing.T) {
	auth := discoveryV2Auth(t, 3, false)
	if auth.Certificates[0].PrincipalURI == "" {
		t.Fatal("fixture lost its principal; this test would prove nothing")
	}
	if auth.Certificates[0].OrganizationID != 0 {
		t.Fatal("fixture has a legacy org id; this test would prove nothing")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	assets, err := fetchCloudAssetsFiltered(ctx, auth, false)
	if err == nil {
		t.Fatalf("expected a refusal for a principal-bearing session, got %d assets and no error", len(assets))
	}
	if assets != nil {
		t.Fatalf("expected no assets alongside the refusal, got %d", len(assets))
	}
	if !strings.Contains(err.Error(), "WDY-3063") {
		t.Errorf("refusal should name the porting ticket so the operator can follow it: %v", err)
	}
	for _, works := range []string{"discover", "tunnel"} {
		if !strings.Contains(err.Error(), works) {
			t.Errorf("refusal should name %q as a command that does work on this session: %v", works, err)
		}
	}
}

// The legacy arm must keep working untouched — the guard keys on the principal,
// not on the org id, so a legacy session with a real org id still lists assets.
func TestFetchCloudAssetsFilteredStillServesLegacySession(t *testing.T) {
	auth := discoveryV2Auth(t, 2, false)
	auth.Certificates[0].PrincipalURI = ""
	auth.Certificates[0].OrganizationID = 42

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := fetchCloudAssetsFiltered(ctx, auth, false)
	if err == nil {
		return // reached and satisfied the v1 service; guard plainly did not fire
	}
	if strings.Contains(err.Error(), "WDY-3063") {
		t.Fatalf("guard fired on a legacy session: %v", err)
	}
	// The stub registers only the v2 service, so the legacy call is expected to
	// end at the transport with "unknown service wendycloud.v1.AssetService".
	// Reaching that error is itself the proof: the guard returns before any
	// dial, so an RPC-layer failure means execution got past it.
	if !strings.Contains(err.Error(), "wendycloud.v1.AssetService") {
		t.Fatalf("expected the legacy call to reach the v1 RPC, got: %v", err)
	}
}
