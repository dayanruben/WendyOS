package interceptor

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func peerAddr(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return "unknown"
	}
	return p.Addr.String()
}

func checkMTLS(ctx context.Context, logger *zap.Logger) error {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		logger.Warn("rejected unauthenticated gRPC caller", zap.String("remote", peerAddr(ctx)))
		return status.Errorf(codes.Unauthenticated, "missing peer credentials")
	}
	if _, ok := p.AuthInfo.(credentials.TLSInfo); !ok {
		logger.Warn("rejected non-mTLS gRPC caller", zap.String("remote", peerAddr(ctx)))
		return status.Errorf(codes.Unauthenticated, "mTLS authentication required")
	}
	return nil
}

// UnaryMTLSInterceptor rejects unary calls that do not carry mTLS peer credentials.
func UnaryMTLSInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := checkMTLS(ctx, logger); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamMTLSInterceptor rejects streaming calls that do not carry mTLS peer credentials.
func StreamMTLSInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := checkMTLS(ss.Context(), logger); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
