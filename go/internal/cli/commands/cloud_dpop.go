//go:build darwin || linux || windows

package commands

import (
	"context"
	"fmt"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
)

// authIsDPoPBound reports whether the session's access token is sender-
// constrained to a stored ML-DSA DPoP key (an OAuth login, post-WDY-3032). Such
// a token must be presented as DPoP with a per-call proof, never as Bearer. A
// self-hosted API-key session or a legacy token session is not bound and keeps
// Bearer.
func authIsDPoPBound(auth *config.AuthConfig) bool {
	return auth != nil && auth.OAuthIssuer != "" && auth.DPoPPrivateKey != ""
}

// dpopDialOptions installs per-RPC DPoP proof injection for a DPoP-bound OAuth
// session. The access token carries cnf.jkt, so it must be presented as
// `Authorization: DPoP <token>` with a fresh RFC 9449 proof per call (RFC 9449
// §7.2) — presenting it as a plain Bearer drops the sender-constraint the
// ML-DSA DPoP login bought (WDY-3107). API-key and legacy token sessions have
// no bound key: they return nil here and keep Bearer (set by cloudContext).
//
// The proof is built per RPC, not per message, and its htu is the invoked
// method path — so it must be built where the method is known, i.e. in a
// client interceptor rather than in cloudContext.
func dpopDialOptions(auth *config.AuthConfig) []grpc.DialOption {
	if !authIsDPoPBound(auth) {
		return nil
	}
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(dpopUnaryInterceptor(auth)),
		grpc.WithChainStreamInterceptor(dpopStreamInterceptor(auth)),
	}
}

// dpopAuthContext refreshes the access token if needed, builds a fresh DPoP
// proof bound to (method, token), and returns a context whose outgoing metadata
// carries `authorization: DPoP <token>` and `dpop: <proof>`. It overwrites any
// authorization header cloudContext may have set, so a bound token never leaves
// as Bearer.
func dpopAuthContext(ctx context.Context, auth *config.AuthConfig, fullMethod string) (context.Context, error) {
	// ensureOAuthAccessToken mutates the session (refresh + persist); snapshot
	// so concurrent RPCs on one dialed conn do not race on shared state.
	local := *auth
	local.Certificates = append([]config.CertificateInfo(nil), auth.Certificates...)
	if err := ensureOAuthAccessToken(ctx, &local); err != nil {
		return nil, err
	}
	token := local.APIKey
	if token == "" {
		return nil, fmt.Errorf("DPoP: OAuth session has no access token")
	}
	keyPEM, err := local.OAuthDPoPKey()
	if err != nil {
		return nil, fmt.Errorf("DPoP: loading bound key: %w", err)
	}
	key, err := certs.ParseSigningPrivateKeyPEM([]byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("DPoP: parsing bound key: %w", err)
	}
	// htu is the RS URL: the broker authority we dialed plus the gRPC method
	// path (fullMethod already begins with "/"). No query or fragment, so it is
	// already RFC 9449-canonical.
	htu := "https://" + local.CloudGRPC + fullMethod
	proof, err := newDPoPAccessProof(key, http.MethodPost, htu, token)
	if err != nil {
		return nil, fmt.Errorf("DPoP: building proof: %w", err)
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
	} else {
		md = metadata.MD{}
	}
	md.Set("authorization", "DPoP "+token)
	md.Set("dpop", proof)
	return metadata.NewOutgoingContext(ctx, md), nil
}

func dpopUnaryInterceptor(auth *config.AuthConfig) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, err := dpopAuthContext(ctx, auth, method)
		if err != nil {
			return err
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func dpopStreamInterceptor(auth *config.AuthConfig) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, err := dpopAuthContext(ctx, auth, method)
		if err != nil {
			return nil, err
		}
		return streamer(ctx, desc, cc, method, opts...)
	}
}
