//go:build darwin || linux || windows

package commands

import (
	"context"
	"crypto/mldsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

const testDPoPMethod = "/wendy.cloud.v2.OrganizationService/ListOrganizations"

// dpopBoundSession builds an OAuth session whose access token is bound to a
// freshly generated ML-DSA DPoP key, with an expiry far enough out that
// ensureOAuthAccessToken short-circuits (no network refresh).
func dpopBoundSession(t *testing.T) (*config.AuthConfig, *mldsa.PublicKey, string) {
	t.Helper()
	keyPEM, err := certs.GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatalf("GenerateMLDSAKeyPair: %v", err)
	}
	signer, err := certs.ParseSigningPrivateKeyPEM([]byte(keyPEM))
	if err != nil {
		t.Fatalf("ParseSigningPrivateKeyPEM: %v", err)
	}
	pub, ok := signer.Public().(*mldsa.PublicKey)
	if !ok {
		t.Fatalf("public key is %T, want *mldsa.PublicKey", signer.Public())
	}
	const token = "header.payload.sig-access-token"
	auth := &config.AuthConfig{
		CloudGRPC:      "api.dev.wendy.sh:443",
		OAuthIssuer:    "https://auth.dev.wendy.sh/realms/acme",
		DPoPPrivateKey: keyPEM,
		APIKey:         token,
		OAuthExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	return auth, pub, token
}

// runUnaryDPoP runs the unary interceptor and returns the authorization and
// dpop headers it injected into the outgoing context.
func runUnaryDPoP(t *testing.T, auth *config.AuthConfig) (authz, proof string) {
	t.Helper()
	var got context.Context
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		got = ctx
		return nil
	}
	if err := dpopUnaryInterceptor(auth)(context.Background(), testDPoPMethod, nil, nil, nil, invoker); err != nil {
		t.Fatalf("unary interceptor: %v", err)
	}
	return dpopHeadersFromCtx(t, got)
}

func runStreamDPoP(t *testing.T, auth *config.AuthConfig) (authz, proof string) {
	t.Helper()
	var got context.Context
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		got = ctx
		return nil, nil
	}
	if _, err := dpopStreamInterceptor(auth)(context.Background(), &grpc.StreamDesc{}, nil, testDPoPMethod, streamer); err != nil {
		t.Fatalf("stream interceptor: %v", err)
	}
	return dpopHeadersFromCtx(t, got)
}

func dpopHeadersFromCtx(t *testing.T, ctx context.Context) (authz, proof string) {
	t.Helper()
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("no outgoing metadata set by the interceptor")
	}
	az := md["authorization"]
	dp := md["dpop"]
	if len(az) != 1 || len(dp) != 1 {
		t.Fatalf("want exactly one authorization and one dpop header, got authz=%v dpop=%v", az, dp)
	}
	return az[0], dp[0]
}

// verifyDPoPProof asserts the proof is a valid ML-DSA JWS bound to the session
// key, and returns its decoded payload.
func verifyDPoPProof(t *testing.T, proof, token string, pub *mldsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		t.Fatalf("proof has %d segments, want 3", len(parts))
	}
	// Signature verifies against the session's bound key.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decoding signature: %v", err)
	}
	signingInput := parts[0] + "." + parts[1]
	if err := mldsa.Verify(pub, []byte(signingInput), sig, nil); err != nil {
		t.Fatalf("proof signature does not verify against the session key: %v", err)
	}
	// Header binds the proof to the session key (cnf.jkt is computed from jwk).
	var header struct {
		Typ string            `json:"typ"`
		JWK map[string]string `json:"jwk"`
	}
	hdrJSON, _ := base64.RawURLEncoding.DecodeString(parts[0])
	if err := json.Unmarshal(hdrJSON, &header); err != nil {
		t.Fatalf("parsing header: %v", err)
	}
	if header.Typ != "dpop+jwt" {
		t.Errorf("typ = %q, want dpop+jwt", header.Typ)
	}
	if header.JWK["kty"] != "AKP" || header.JWK["alg"] != "ML-DSA-65" {
		t.Errorf("jwk = %v, want an RFC 9964 AKP ML-DSA-65 key", header.JWK)
	}
	var payload map[string]any
	plJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(plJSON, &payload); err != nil {
		t.Fatalf("parsing payload: %v", err)
	}
	// ath binds the proof to these exact access-token bytes.
	sum := sha256.Sum256([]byte(token))
	if payload["ath"] != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Errorf("ath = %v, want base64url(sha256(access token))", payload["ath"])
	}
	if payload["htm"] != "POST" {
		t.Errorf("htm = %v, want POST", payload["htm"])
	}
	return payload
}

func TestDPoPUnaryInterceptorSendsProofNotBearer(t *testing.T) {
	auth, pub, token := dpopBoundSession(t)
	authz, proof := runUnaryDPoP(t, auth)

	if authz != "DPoP "+token {
		t.Fatalf("authorization = %q, want %q (Bearer must never be emitted for a bound token)", authz, "DPoP "+token)
	}
	if strings.HasPrefix(authz, "Bearer ") {
		t.Fatal("Bearer emitted for a cnf-bound token")
	}
	payload := verifyDPoPProof(t, proof, token, pub)
	if got := payload["htu"]; got != "https://"+auth.CloudGRPC+testDPoPMethod {
		t.Errorf("htu = %v, want the invoked method path %q", got, "https://"+auth.CloudGRPC+testDPoPMethod)
	}
}

func TestDPoPStreamInterceptorSendsProofPerRPC(t *testing.T) {
	auth, pub, token := dpopBoundSession(t)
	authz, proof := runStreamDPoP(t, auth)

	if authz != "DPoP "+token {
		t.Fatalf("stream authorization = %q, want DPoP", authz)
	}
	payload := verifyDPoPProof(t, proof, token, pub)
	if got := payload["htu"]; got != "https://"+auth.CloudGRPC+testDPoPMethod {
		t.Errorf("stream htu = %v, want %q", got, "https://"+auth.CloudGRPC+testDPoPMethod)
	}
}

// Each RPC gets a fresh proof: distinct jti even for the same method/token.
func TestDPoPProofIsFreshPerCall(t *testing.T) {
	auth, _, _ := dpopBoundSession(t)
	_, p1 := runUnaryDPoP(t, auth)
	_, p2 := runUnaryDPoP(t, auth)
	jti := func(proof string) any {
		var payload map[string]any
		pl, _ := base64.RawURLEncoding.DecodeString(strings.Split(proof, ".")[1])
		_ = json.Unmarshal(pl, &payload)
		return payload["jti"]
	}
	if jti(p1) == jti(p2) {
		t.Fatal("jti repeated across calls; proof is not fresh per RPC")
	}
}

// cloudContext must not emit Bearer for a bound token (the interceptor carries
// DPoP), but must still emit Bearer for an unbound API-key session.
func TestCloudContextBearerOnlyForUnboundSessions(t *testing.T) {
	t.Run("bound OAuth session: no Bearer", func(t *testing.T) {
		auth, _, _ := dpopBoundSession(t)
		ctx, err := cloudContext(context.Background(), auth)
		if err != nil {
			t.Fatalf("cloudContext: %v", err)
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		if az := md["authorization"]; len(az) != 0 {
			t.Fatalf("authorization = %v, want none (bound token must go via the DPoP interceptor)", az)
		}
	})
	t.Run("unbound API-key session: Bearer", func(t *testing.T) {
		auth := &config.AuthConfig{CloudGRPC: "self.example:443", APIKey: "api-key-123"}
		ctx, err := cloudContext(context.Background(), auth)
		if err != nil {
			t.Fatalf("cloudContext: %v", err)
		}
		md, _ := metadata.FromOutgoingContext(ctx)
		if az := md["authorization"]; len(az) != 1 || az[0] != "Bearer api-key-123" {
			t.Fatalf("authorization = %v, want [Bearer api-key-123]", az)
		}
	})
}
