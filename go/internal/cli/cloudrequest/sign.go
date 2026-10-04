// Package cloudrequest signs privileged Wendy Cloud RPCs with the operator
// certificate obtained from pki-core.
package cloudrequest

import (
	"crypto"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	cloudpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/cloudpb/v2"
	"google.golang.org/protobuf/proto"
)

const (
	brokerAudience = "https://cloud.wendy.sh/broker"
	signatureTTL   = 30 * time.Second
)

// Signer creates the JWS request descriptors required by Wendy Cloud for
// operator-privileged mutations. A Signer is safe for concurrent RPCs: it
// holds immutable key material and obtains fresh randomness for each request.
type Signer struct {
	privateKey crypto.Signer
	tenantUUID string
	x5c        []string
	audience   string
	now        func() time.Time
	random     io.Reader
}

// SignRequest wraps req in the wendycloud.v2.SignedRequest envelope that the
// operator-signed method takes (WDY-3458): payload is req serialized once,
// payload_type its message name, and signature a JWS whose body_sha256 binds
// exactly those payload bytes. The signature travels in the body; nothing is
// added to the call's metadata.
func SignRequest(auth *config.AuthConfig, method string, req proto.Message) (*cloudpbv2.SignedRequest, error) {
	s, err := newSigner(auth)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("%s requires an operator certificate; run 'wendy auth login'", method)
	}
	return s.signRequest(method, req)
}

func newSigner(auth *config.AuthConfig) (*Signer, error) {
	if auth == nil || len(auth.Certificates) == 0 {
		return nil, nil
	}
	certInfo := auth.Certificates[0]
	// Operator identity is a property of the certificate, not of the login
	// mechanism that happened to obtain it. In particular, imported/migrated
	// operator credentials may not carry the OAuthIssuer bookkeeping field.
	if certInfo.PrincipalURI == "" {
		return nil, nil
	}
	tenantUUID, err := operatorTenant(certInfo.PrincipalURI)
	if err != nil {
		return nil, fmt.Errorf("loading Cloud request-signing identity: %w", err)
	}
	privateKeyPEM, err := certInfo.PrivateKeyPEM()
	if err != nil {
		return nil, fmt.Errorf("loading Cloud request-signing key: %w", err)
	}
	pair, err := certs.TLSKeyPair(certInfo.PemCertificate, certInfo.PemCertificateChain, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading Cloud request-signing certificate: %w", err)
	}
	// WDY-3032 is a hard cutover: the operator credential is ML-DSA-65, with
	// no ECDSA fallback. A session predating it is refused here rather than
	// signed with, so the failure names the fix instead of surfacing later as
	// a rejected signature from Cloud.
	privateKey, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("Cloud request signing key of type %T cannot sign", pair.PrivateKey)
	}
	if _, ok := privateKey.Public().(*mldsa.PublicKey); !ok {
		return nil, fmt.Errorf("Cloud request signing requires an ML-DSA-65 operator key, but this session holds %T; re-run 'wendy auth login'", privateKey.Public())
	}
	x5c := make([]string, 0, len(pair.Certificate))
	for _, der := range pair.Certificate {
		x5c = append(x5c, base64.StdEncoding.EncodeToString(der))
	}
	return &Signer{
		privateKey: privateKey,
		tenantUUID: tenantUUID,
		x5c:        x5c,
		audience:   brokerAudience,
		now:        time.Now,
		random:     rand.Reader,
	}, nil
}

// operatorTenant reads the tenant a session's principal belongs to.
//
// It used to insist on the kind "operator", which is what the AAA contract
// §5.2 says pki-core's own identity endpoint stamps — but cloud relays its
// leaves through the service-identity profile and stamps "service/user-<id>",
// so a cloud-issued session was refused here for spelling. Both are legitimate
// human-operator identities under the contract (D17 makes a service account a
// normal user behind a different front door), so both are accepted and
// certs.ParsePrincipal is the single place that decides so.
//
// A device or code-signing principal is still refused: neither is an actor
// that may sign a privileged cloud mutation.
func operatorTenant(principal string) (string, error) {
	id, err := certs.ParsePrincipal(principal)
	if err != nil {
		return "", fmt.Errorf("operator certificate has invalid principal URI %q: %w", principal, err)
	}
	if id.EntityType != certs.EntityUser {
		return "", fmt.Errorf("operator certificate principal %q is a %s, not an operator", principal, id.EntityType)
	}
	return id.TenantUUID, nil
}

func (s *Signer) signRequest(method string, req proto.Message) (*cloudpbv2.SignedRequest, error) {
	resource, ok := signedResources[method]
	if !ok {
		return nil, requestTypeError(method, req)
	}
	target, ok := resource(s.tenantUUID, req)
	if !ok {
		return nil, requestTypeError(method, req)
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling Cloud request %s: %w", method, err)
	}
	sum := sha256.Sum256(payload)
	jws, err := s.sign(strings.TrimPrefix(method, "/"), target, base64.RawURLEncoding.EncodeToString(sum[:]))
	if err != nil {
		return nil, fmt.Errorf("signing Cloud request %s: %w", method, err)
	}
	return &cloudpbv2.SignedRequest{
		Payload:     payload,
		PayloadType: string(req.ProtoReflect().Descriptor().FullName()),
		Signature:   []byte(jws),
	}, nil
}

// signedResources maps each operator-signed method the CLI calls to its
// target.resource (cloud RequestSigning.md). The type assertion is what keeps
// payload_type equal to the method's request message.
var signedResources = map[string]func(tenant string, req proto.Message) (string, bool){
	cloudpbv2.DeviceEnrollmentService_EnrollDevice_FullMethodName: func(tenant string, req proto.Message) (string, bool) {
		in, ok := req.(*cloudpbv2.EnrollDeviceRequest)
		return "org/" + tenant + "/device/" + in.GetDeviceId(), ok
	},
}

func requestTypeError(method string, req proto.Message) error {
	return fmt.Errorf("cannot sign Cloud request %s with message type %T", method, req)
}

func (s *Signer) sign(operation, resource, bodyDigest string) (string, error) {
	nonceBytes := make([]byte, 32)
	if _, err := io.ReadFull(s.random, nonceBytes); err != nil {
		return "", fmt.Errorf("generating request nonce: %w", err)
	}
	now := s.now().Unix()
	descriptor := map[string]any{
		"aud":         s.audience,
		"body_sha256": bodyDigest,
		// One id per call: the CLI is the flow's origin, so it mints it.
		"correlation_id": uuid.NewString(),
		"expiry":         now + int64(signatureTTL/time.Second),
		"iat":            now,
		"nonce":          base64.RawURLEncoding.EncodeToString(nonceBytes),
		"operation":      operation,
		"target": map[string]any{
			"resource": resource,
			"tenant":   s.tenantUUID,
		},
	}
	payload, err := canonicalJSON(descriptor)
	if err != nil {
		return "", fmt.Errorf("encoding request descriptor: %w", err)
	}
	// The contract's x5c is exactly one entry, the leaf: Cloud validates only
	// x5c[0] through PKI, which owns the issuer chain.
	return s.signPayload(payload, s.x5c[:1])
}

// EnrollmentRequest signs PKI's enrollment authority separately from the Cloud
// RPC descriptor. PKI verifies this JWS against the tenant's Operator Authority.
func EnrollmentRequest(auth *config.AuthConfig, deviceID string) ([]byte, error) {
	s, err := newSigner(auth)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("device enrollment requires an operator certificate")
	}
	now := s.now().Unix()
	payload, err := canonicalJSON(map[string]any{
		"tenant": s.tenantUUID, "device_id": deviceID, "device_class": "B",
		"iat": now, "exp": now + 300, "jti": uuid.NewString(),
	})
	if err != nil {
		return nil, err
	}
	// This artifact is carried in the protobuf body, not HTTP metadata. Keep
	// the full chain for PKI's enrollment signature verifier.
	jws, err := s.signPayload(payload, s.x5c)
	return []byte(jws), err
}

func (s *Signer) signPayload(payload []byte, chain []string) (string, error) {
	header, err := canonicalJSON(map[string]any{"alg": "ML-DSA-65", "x5c": chain})
	if err != nil {
		return "", fmt.Errorf("encoding JWS header: %w", err)
	}
	protected := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := protected + "." + encodedPayload

	// ML-DSA signs the input bytes directly with empty Options — the same shape
	// pki-core verifies with (reqsig: Verify(pk, signingInput, sig, nil)). No
	// pre-hash, and the JWS signature is the raw FIPS-204 value.
	key, ok := s.privateKey.(*mldsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("request-signing key is %T, want *mldsa.PrivateKey", s.privateKey)
	}
	signature, err := key.Sign(s.random, []byte(signingInput), &mldsa.Options{})
	if err != nil {
		return "", fmt.Errorf("signing descriptor: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// canonicalJSON is sufficient for the request descriptor's deliberately
// narrow RFC 8785 subset: string-keyed objects, strings, arrays, and integers.
// encoding/json sorts map keys; disabling HTML escaping preserves JCS strings.
func canonicalJSON(value any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}
