package certs

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
)

const (
	// AnnotationSignature is the OCI manifest annotation key for the ECDSA-P256
	// signature over the entitlement annotations.
	AnnotationSignature = "sh.wendy/signature"
	// AnnotationSignatureCert is the OCI manifest annotation key for the PEM-encoded
	// leaf certificate whose private key produced the AnnotationSignature.
	AnnotationSignatureCert = "sh.wendy/signature.cert"

	// entitlementAnnotationPrefix is the prefix for Wendy entitlement annotations.
	// Duplicated here to avoid importing the CLI package from the shared certs package.
	entitlementAnnotationPrefix = "sh.wendy/entitlement."
)

// SignBytes signs data with ECDSA-P256+SHA-256 and returns the DER signature
// encoded as standard base64.
func SignBytes(data []byte, key *ecdsa.PrivateKey) (string, error) {
	hash := sha256.Sum256(data)
	sigDER, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		return "", fmt.Errorf("signing: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sigDER), nil
}

// VerifyBytes verifies an ECDSA signature produced by SignBytes.
// sigB64 must be the base64-encoded DER signature; cert must carry an EC public key.
func VerifyBytes(data []byte, sigB64 string, cert *x509.Certificate) error {
	sigDER, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("certificate does not contain an EC public key")
	}
	hash := sha256.Sum256(data)
	if !ecdsa.VerifyASN1(pub, hash[:], sigDER) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// ParseLeafCertificate decodes the first CERTIFICATE block from a PEM string.
func ParseLeafCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no CERTIFICATE block found in PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}
	return cert, nil
}

// SigningPayload builds the canonical byte payload that is signed and verified
// for a manifest. The payload is the concatenation of:
//
//  1. Content digests (sorted lexicographically), each as "digest=<value>\n".
//     These are the config + layer digests for single manifests, or child
//     manifest digests for OCI indexes. They bind the signature to the actual
//     image content so that swapping layers invalidates the signature.
//  2. All sh.wendy/entitlement.* annotation key/value pairs, sorted
//     lexicographically, each as "key=value\n".
//
// The sh.wendy/signature* annotations are excluded so the payload is stable
// whether or not the manifest has already been signed.
func SigningPayload(contentDigests []string, annotations map[string]string) []byte {
	var buf strings.Builder

	sorted := make([]string, len(contentDigests))
	copy(sorted, contentDigests)
	sort.Strings(sorted)
	for _, d := range sorted {
		buf.WriteString("digest=")
		buf.WriteString(d)
		buf.WriteByte('\n')
	}

	var keys []string
	for k := range annotations {
		if strings.HasPrefix(k, entitlementAnnotationPrefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(annotations[k])
		buf.WriteByte('\n')
	}

	return []byte(buf.String())
}
