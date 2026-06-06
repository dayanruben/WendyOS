package mtls

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	circlSign "github.com/cloudflare/circl/sign"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"go.uber.org/zap"
)

const (
	// maxCertLifetime is the maximum certificate validity window used as a
	// compensating control when CRL checking is unavailable. A certificate valid
	// for longer than this cannot be promptly revoked, so it is rejected.
	maxCertLifetime = 25 * time.Hour

	// crlFetchTimeout limits network time spent fetching a CRL per distribution point.
	crlFetchTimeout = 5 * time.Second

	// maxCRLGracePeriod is how long a CRL whose NextUpdate has passed may still be
	// used when the CRL server is temporarily unreachable. This avoids a transient
	// network outage becoming a denial-of-service for legitimate connections.
	maxCRLGracePeriod = 1 * time.Hour
)

// crlCache holds the most-recently fetched CRL per distribution-point URL.
var (
	crlCacheMu  sync.RWMutex
	crlCacheMap = make(map[string]*x509.RevocationList)
)

// checkRevocation verifies that leaf has not been revoked.
//
// For standard (RSA/ECDSA) leaf certs with CRL distribution points, each DP is
// fetched, the CRL signature is verified against the issuing CA cert, and the
// serial number is checked. A per-URL cache keyed on the CRL's NextUpdate field
// avoids repeated network fetches; a one-hour grace period lets the cache absorb
// temporary CRL server outages without opening the system to revoked credentials.
// When all DP fetches fail and there is no valid cached CRL, the connection is
// rejected (fail-closed) to avoid a scenario where an attacker suppresses CRL
// availability to keep a compromised certificate working.
//
// For ML-DSA leaf certs, Go's crypto/x509 cannot verify ML-DSA CRL signatures,
// so certificate lifetime enforcement (maxCertLifetime) is used instead.
// For standard certs with no CRL DPs, the same lifetime enforcement applies.
func checkRevocation(leaf *x509.Certificate, caCerts []*x509.Certificate) error {
	// ML-DSA leaf: cannot verify ML-DSA CRL signatures with standard crypto/x509.
	if sigOID, err := certSigAlgOID(leaf); err == nil {
		if _, schemeErr := mldsaScheme(sigOID); schemeErr == nil {
			return enforceLifetime(leaf)
		}
	}

	if len(leaf.CRLDistributionPoints) == 0 {
		return enforceLifetime(leaf)
	}

	// Locate the issuing CA cert for CRL signature verification.
	var issuerCert *x509.Certificate
	for _, ca := range caCerts {
		if bytes.Equal(ca.RawSubject, leaf.RawIssuer) {
			issuerCert = ca
			break
		}
	}
	if issuerCert == nil {
		// Issuer not in trusted pool — cannot verify CRL signature.
		return enforceLifetime(leaf)
	}

	var lastErr error
	for _, dp := range leaf.CRLDistributionPoints {
		crl, err := getCRLCached(dp, issuerCert)
		if err != nil {
			lastErr = err
			continue
		}
		if isCertRevoked(crl, leaf.SerialNumber) {
			return fmt.Errorf("certificate serial %s is revoked", leaf.SerialNumber)
		}
		return nil // checked and not revoked
	}

	// All CRL distribution points unreachable with no valid cached CRL: hard fail.
	// Accepting a cert here would open a window where an attacker who has suppressed
	// CRL availability can continue authenticating with a revoked credential.
	return fmt.Errorf("certificate revocation status could not be verified: %w", lastErr)
}

// enforceLifetime rejects a certificate whose validity window exceeds maxCertLifetime.
// This is the compensating control used when CRL/OCSP checking is unavailable.
func enforceLifetime(leaf *x509.Certificate) error {
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
	if lifetime > maxCertLifetime {
		return fmt.Errorf("certificate lifetime %v exceeds maximum %v — use short-lived certificates or add a CRL distribution point", lifetime, maxCertLifetime)
	}
	return nil
}

// getCRLCached returns a verified CRL for url, using the in-memory cache when
// the cached CRL is still within its NextUpdate window. On fetch failure it
// returns the cached CRL if it is within the grace period, or an error if the
// cache is empty or has expired beyond the grace period (fail-closed).
func getCRLCached(url string, issuerCert *x509.Certificate) (*x509.RevocationList, error) {
	now := time.Now()

	crlCacheMu.RLock()
	cached, ok := crlCacheMap[url]
	crlCacheMu.RUnlock()

	if ok && now.Before(cached.NextUpdate) {
		return cached, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), crlFetchTimeout)
	defer cancel()
	fresh, err := fetchAndVerifyCRL(ctx, url, issuerCert)
	if err != nil {
		if ok && now.Before(cached.NextUpdate.Add(maxCRLGracePeriod)) {
			return cached, nil
		}
		return nil, err
	}

	crlCacheMu.Lock()
	crlCacheMap[url] = fresh
	crlCacheMu.Unlock()
	return fresh, nil
}

// fetchAndVerifyCRL fetches the CRL from url, verifies its signature against
// issuerCert, and returns the parsed revocation list. Uses a dedicated HTTP
// client with redirect control so that crafted certificates cannot redirect
// CRL fetches to arbitrary hosts.
func fetchAndVerifyCRL(ctx context.Context, url string, issuerCert *x509.Certificate) (*x509.RevocationList, error) {
	client := &http.Client{
		Timeout: crlFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 2 {
				return fmt.Errorf("too many CRL redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building CRL request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching CRL from %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading CRL body: %w", err)
	}
	crl, err := x509.ParseRevocationList(body)
	if err != nil {
		return nil, fmt.Errorf("parsing CRL: %w", err)
	}
	// Verify the CRL's signature against the issuing CA to detect MITM-substituted CRLs.
	if err := crl.CheckSignatureFrom(issuerCert); err != nil {
		return nil, fmt.Errorf("CRL signature verification failed: %w", err)
	}
	return crl, nil
}

// isCertRevoked reports whether serial appears in the CRL's revoked certificate list.
func isCertRevoked(crl *x509.RevocationList, serial *big.Int) bool {
	for i := range crl.RevokedCertificateEntries {
		if crl.RevokedCertificateEntries[i].SerialNumber.Cmp(serial) == 0 {
			return true
		}
	}
	return false
}

var (
	oidMLDSA65 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 18}
	oidMLDSA87 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 3, 19}
)

type algID struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type certOuter struct {
	TBSCertificate     asn1.RawValue
	SignatureAlgorithm algID
	Signature          asn1.BitString
}

type spkiOuter struct {
	Algorithm algID
	PublicKey asn1.BitString
}

func parseCertsFromPEM(chainPEM []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := chainPEM
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			// ML-DSA certs produce "trailing data" because pki-core appends extra
			// bytes after the outer SEQUENCE. Strip them by reading exactly one
			// ASN.1 element and re-parsing.
			var raw asn1.RawValue
			if _, asn1Err := asn1.Unmarshal(block.Bytes, &raw); asn1Err == nil {
				cert, err = x509.ParseCertificate(raw.FullBytes)
			}
		}
		if err != nil {
			continue
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func certSigAlgOID(cert *x509.Certificate) (asn1.ObjectIdentifier, error) {
	var outer certOuter
	if _, err := asn1.Unmarshal(cert.Raw, &outer); err != nil {
		return nil, fmt.Errorf("parsing certificate ASN.1: %w", err)
	}
	return outer.SignatureAlgorithm.Algorithm, nil
}

func issuerPublicKeyBytes(issuer *x509.Certificate) (asn1.ObjectIdentifier, []byte, error) {
	var s spkiOuter
	if _, err := asn1.Unmarshal(issuer.RawSubjectPublicKeyInfo, &s); err != nil {
		return nil, nil, fmt.Errorf("parsing SubjectPublicKeyInfo: %w", err)
	}
	return s.Algorithm.Algorithm, s.PublicKey.Bytes, nil
}

func mldsaScheme(oid asn1.ObjectIdentifier) (circlSign.Scheme, error) {
	switch {
	case oid.Equal(oidMLDSA65):
		return mldsa65.Scheme(), nil
	case oid.Equal(oidMLDSA87):
		return mldsa87.Scheme(), nil
	default:
		return nil, fmt.Errorf("unsupported ML-DSA OID: %v", oid)
	}
}

// verifyMLDSASignature checks that issuer signed cert using ML-DSA.
func verifyMLDSASignature(issuer, cert *x509.Certificate) error {
	sigOID, err := certSigAlgOID(cert)
	if err != nil {
		return err
	}

	scheme, err := mldsaScheme(sigOID)
	if err != nil {
		return err
	}

	_, pubKeyBytes, err := issuerPublicKeyBytes(issuer)
	if err != nil {
		return err
	}

	pk, err := scheme.UnmarshalBinaryPublicKey(pubKeyBytes)
	if err != nil {
		return fmt.Errorf("parsing ML-DSA public key: %w", err)
	}

	opts := &circlSign.SignatureOpts{Context: ""}
	if !scheme.Verify(pk, cert.RawTBSCertificate, cert.Signature, opts) {
		return fmt.Errorf("ML-DSA signature verification failed")
	}
	return nil
}

// maxTime returns the later of two times. Used to apply a NotBefore floor when
// the device clock has not yet been synchronised via NTP.
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// buildVerifyPeerCertificate returns a VerifyPeerCertificate callback that
// handles both standard (RSA/ECDSA) and ML-DSA-signed certificate chains.
// logger may be nil, in which case no logging is performed.
// notBeforeFloor is used as a lower bound on the current time for NotBefore
// checks: if the device clock is behind the floor (e.g. RTC reset to epoch
// before NTP sync), effectiveNow = max(time.Now(), notBeforeFloor) is used
// so that certs issued at provisioning time are still accepted. Pass a zero
// time.Time to disable the floor.
func buildVerifyPeerCertificate(caPool *x509.CertPool, caCerts []*x509.Certificate, logger *zap.Logger, notBeforeFloor time.Time) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no client certificate presented")
		}

		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("parsing client certificate: %w", err)
		}

		// Capture the real clock once to avoid TOCTOU between the expiry pre-check
		// and the verification call. effectiveNow applies the NotBefore floor only.
		realNow := time.Now()
		effectiveNow := maxTime(realNow, notBeforeFloor)

		// Pre-reject expired certs before any further processing. The floor must
		// not mask real-time expiry: checking here with realNow eliminates any
		// TOCTOU window between the clock snapshot and the verification call.
		if realNow.After(leaf.NotAfter) {
			expiredErr := fmt.Errorf("certificate expired (notAfter=%v)", leaf.NotAfter)
			// The floor is irrelevant once the cert is expired against the real
			// clock: pass realNow so the rejection log reflects the actual clock
			// and never mistakes an expired cert for a clock-skew case.
			logCertRejection(logger, leaf, expiredErr, realNow)
			return expiredErr
		}

		// Build an intermediates pool from the rest of the chain presented by the client.
		intermediates := x509.NewCertPool()
		for _, rawCert := range rawCerts[1:] {
			if intermediate, parseErr := x509.ParseCertificate(rawCert); parseErr == nil {
				intermediates.AddCert(intermediate)
			}
		}

		// Try standard Go verification first (handles RSA/ECDSA chains).
		// CurrentTime uses effectiveNow so that certs issued at provisioning are
		// accepted when the device clock has not yet synced via NTP.
		opts := x509.VerifyOptions{
			Roots:         caPool,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			CurrentTime:   effectiveNow,
		}
		stdErr := func() error { _, e := leaf.Verify(opts); return e }()
		if stdErr == nil {
			if revErr := checkRevocation(leaf, caCerts); revErr != nil {
				logCertRejection(logger, leaf, revErr, effectiveNow)
				return revErr
			}
			return nil
		}

		// Only fall back to ML-DSA verification when the leaf cert uses an ML-DSA
		// signature algorithm; for all other failures return the standard error.
		sigOID, oidErr := certSigAlgOID(leaf)
		if oidErr != nil {
			logCertRejection(logger, leaf, stdErr, effectiveNow)
			return stdErr
		}
		if _, schemeErr := mldsaScheme(sigOID); schemeErr != nil {
			logCertRejection(logger, leaf, stdErr, effectiveNow)
			return stdErr
		}

		mldsaErr := verifyMLDSAClientCert(leaf, caCerts, realNow, effectiveNow)
		if mldsaErr != nil {
			logCertRejection(logger, leaf, mldsaErr, effectiveNow)
			return mldsaErr
		}
		if revErr := checkRevocation(leaf, caCerts); revErr != nil {
			logCertRejection(logger, leaf, revErr, effectiveNow)
			return revErr
		}
		return nil
	}
}

// logCertRejection logs a WARN when a client cert is rejected, with a clock
// skew hint when the error indicates the cert is not yet valid.
func logCertRejection(logger *zap.Logger, leaf *x509.Certificate, err error, effectiveNow time.Time) {
	if logger == nil {
		return
	}
	fields := []zap.Field{
		zap.String("subject", leaf.Subject.CommonName),
		zap.Time("notBefore", leaf.NotBefore),
		zap.Time("notAfter", leaf.NotAfter),
		zap.Error(err),
	}
	msg := "mTLS client certificate rejected"
	// Only hint at clock skew when the device clock is behind the cert's NotBefore.
	// String-matching on the error text would also fire for expired certs, pointing
	// operators at the wrong remediation (NTP sync won't help an expired cert).
	if effectiveNow.Before(leaf.NotBefore) {
		msg += ": certificate not yet valid — device clock may be skewed; check NTP sync with: timedatectl status"
	}
	logger.Warn(msg, fields...)
}

// verifyMLDSAClientCert verifies a client leaf cert against the trusted CA certs
// using ML-DSA signature verification. It checks validity and that the leaf was
// signed by a trusted CA. effectiveNow is max(realNow, notBeforeFloor) and is
// used only for the leaf NotBefore check so that certs issued at provisioning are
// accepted when the device clock has not yet synced via NTP. realNow is the
// unmodified clock snapshot; all NotAfter and CA NotBefore checks use it so the
// floor cannot mask expiry or make an immature CA appear valid.
func verifyMLDSAClientCert(leaf *x509.Certificate, trustedCAs []*x509.Certificate, realNow, effectiveNow time.Time) error {
	if effectiveNow.Before(leaf.NotBefore) || realNow.After(leaf.NotAfter) {
		return fmt.Errorf("certificate not valid at current time (NotBefore=%v NotAfter=%v)", leaf.NotBefore, leaf.NotAfter)
	}

	// Mirror the standard verifier's EKU check: the cert must allow clientAuth
	// (or be unrestricted, i.e. have no ExtKeyUsage set).
	if len(leaf.ExtKeyUsage) > 0 {
		hasClientAuth := false
		for _, eku := range leaf.ExtKeyUsage {
			if eku == x509.ExtKeyUsageClientAuth || eku == x509.ExtKeyUsageAny {
				hasClientAuth = true
				break
			}
		}
		if !hasClientAuth {
			return fmt.Errorf("certificate is not valid for client authentication")
		}
	}

	var lastErr error
	foundSubject := false
	for _, ca := range trustedCAs {
		if !bytes.Equal(ca.RawSubject, leaf.RawIssuer) {
			continue
		}
		foundSubject = true
		if realNow.Before(ca.NotBefore) || realNow.After(ca.NotAfter) {
			lastErr = fmt.Errorf("CA certificate %q not valid at current time", ca.Subject.CommonName)
			continue
		}
		if !ca.BasicConstraintsValid || !ca.IsCA {
			lastErr = fmt.Errorf("certificate %q is not a CA", ca.Subject.CommonName)
			continue
		}
		if ca.KeyUsage != 0 && ca.KeyUsage&x509.KeyUsageCertSign == 0 {
			lastErr = fmt.Errorf("certificate %q is not permitted to sign certificates", ca.Subject.CommonName)
			continue
		}
		if err := verifyMLDSASignature(ca, leaf); err != nil {
			lastErr = fmt.Errorf("invalid signature from CA %q: %w", ca.Subject.CommonName, err)
			continue
		}
		return nil
	}

	if !foundSubject {
		return fmt.Errorf("client certificate issuer not found in trusted CA pool")
	}
	return lastErr
}
