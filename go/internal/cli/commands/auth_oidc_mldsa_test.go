package commands

import (
	"crypto/mldsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
)

func mldsaOperatorKey(t *testing.T) *mldsa.PrivateKey {
	t.Helper()
	keyPEM, err := certs.GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := certs.ParseSigningPrivateKeyPEM([]byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	key, ok := signer.(*mldsa.PrivateKey)
	if !ok {
		t.Fatalf("operator key is %T, want *mldsa.PrivateKey", signer)
	}
	return key
}

// The DPoP proof is what pki-core checks against the token's cnf.jkt, so the
// header alg, the JWK members and the signature form are all wire contract.
// pki-core verifies with cryptomldsa.Verify(pk, signingInput, sig, nil) — no
// pre-hash, empty options — so this asserts exactly that shape.
func TestDPoPProofIsMLDSAAndVerifies(t *testing.T) {
	key := mldsaOperatorKey(t)

	proof, err := newDPoPAccessProof(key, "POST", "https://identity.example/v1/identity/certificate", "token-abc")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		t.Fatalf("proof has %d segments, want 3", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Typ string            `json:"typ"`
		Alg string            `json:"alg"`
		JWK map[string]string `json:"jwk"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	if header.Alg != "ML-DSA-65" {
		t.Errorf("alg = %q, want ML-DSA-65 (pki-core reqsig.AlgMLDSA65)", header.Alg)
	}
	if header.Typ != "dpop+jwt" {
		t.Errorf("typ = %q, want dpop+jwt", header.Typ)
	}
	if header.JWK["kty"] != "AKP" {
		t.Errorf("jwk.kty = %q, want AKP (RFC 9964)", header.JWK["kty"])
	}
	if header.JWK["alg"] != "ML-DSA-65" {
		t.Errorf("jwk.alg = %q; AKP thumbprints include alg, so it must be present", header.JWK["alg"])
	}
	if _, unwanted := header.JWK["crv"]; unwanted {
		t.Error("jwk carries crv; that is an EC member and changes the thumbprint")
	}
	wantPub := base64.RawURLEncoding.EncodeToString(key.Public().(*mldsa.PublicKey).Bytes())
	if header.JWK["pub"] != wantPub {
		t.Error("jwk.pub is not the raw FIPS-204 public key")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	signingInput := parts[0] + "." + parts[1]
	if err := mldsa.Verify(key.Public().(*mldsa.PublicKey), []byte(signingInput), sig, nil); err != nil {
		t.Fatalf("signature does not verify the way pki-core verifies it: %v", err)
	}
}

// cnf.jkt is a byte-exact comparison on the server, and member order is part
// of the hash input — so pin the canonical JSON, not just the parsed members.
func TestOperatorJWKThumbprintCanonicalForm(t *testing.T) {
	key := mldsaOperatorKey(t)

	got, err := operatorJWKThumbprint(key)
	if err != nil {
		t.Fatal(err)
	}
	pub := base64.RawURLEncoding.EncodeToString(key.Public().(*mldsa.PublicKey).Bytes())
	canonical := `{"alg":"ML-DSA-65","kty":"AKP","pub":"` + pub + `"}`
	sum := sha256.Sum256([]byte(canonical))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got != want {
		t.Errorf("thumbprint = %s, want %s (RFC 7638 over {alg,kty,pub} in lexicographic order)", got, want)
	}
}

// An EC session written before the cutover must keep signing until it is
// renewed; only generation moved to ML-DSA.
func TestDPoPStillSignsLegacyECSessions(t *testing.T) {
	keyPEM, err := certs.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := certs.ParseSigningPrivateKeyPEM([]byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	proof, err := newDPoPAccessProof(signer, "POST", "https://identity.example/v1/identity/certificate", "token-abc")
	if err != nil {
		t.Fatal(err)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(strings.Split(proof, ".")[0])
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Alg string            `json:"alg"`
		JWK map[string]string `json:"jwk"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	if header.Alg != "ES256" || header.JWK["kty"] != "EC" {
		t.Errorf("legacy EC session produced alg=%q kty=%q, want ES256/EC", header.Alg, header.JWK["kty"])
	}
}
