package certs

import (
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

// The operator key is ML-DSA-65 and its CSR must be signed with it — that is
// the whole point of WDY-3032, and pki-core binds the CSR key to the token's
// cnf.jkt, so a CSR that silently came out ECDSA would be rejected at issuance.
func TestGenerateMLDSAKeyPairSignsItsOwnCSR(t *testing.T) {
	keyPEM, err := GenerateMLDSAKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil || block.Type != "PRIVATE KEY" {
		t.Fatalf("want a PKCS#8 PRIVATE KEY block, got %q", block)
	}
	// Seed-form PKCS#8 keeps the config.json footprint small; an expanded key
	// would be kilobytes. Guard the encoding, not just the parse.
	if len(keyPEM) > 512 {
		t.Errorf("ML-DSA key PEM is %d bytes; expected the ~128-byte seed form", len(keyPEM))
	}

	csrPEM, err := GenerateCSR([]byte(keyPEM), "operator", []string{"spiffe://wendy.sh/tenant/t/operator/o"})
	if err != nil {
		t.Fatal(err)
	}
	csrBlock, _ := pem.Decode([]byte(csrPEM))
	if csrBlock == nil {
		t.Fatal("CSR is not PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("CSR does not verify under its own key: %v", err)
	}
	if got := csr.SignatureAlgorithm.String(); !strings.Contains(got, "ML-DSA") {
		t.Errorf("CSR signature algorithm = %s, want ML-DSA", got)
	}
	if len(csr.URIs) != 1 || csr.URIs[0].String() != "spiffe://wendy.sh/tenant/t/operator/o" {
		t.Errorf("SPIFFE principal lost from the CSR: %v", csr.URIs)
	}
}

// The EC generation must keep working unchanged — device enroll and os install
// still mint P-256 CSRs through the same function.
func TestGenerateCSRStillAcceptsECKeys(t *testing.T) {
	keyPEM, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := GenerateCSR([]byte(keyPEM), "device", nil)
	if err != nil {
		t.Fatal(err)
	}
	csrBlock, _ := pem.Decode([]byte(csrPEM))
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("EC CSR does not verify: %v", err)
	}
	if strings.Contains(csr.SignatureAlgorithm.String(), "ML-DSA") {
		t.Errorf("EC key produced an ML-DSA CSR: %s", csr.SignatureAlgorithm)
	}
}
