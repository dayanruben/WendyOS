package services

import (
	"go.uber.org/zap"
	"google.golang.org/grpc"

	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
	otelpb "github.com/wendylabsinc/wendy/go/proto/gen/otelpb"
)

// TelemetryServiceV2 implements agentpbv2.WendyTelemetryServiceServer by
// forwarding telemetry events from the shared TelemetryBroadcaster.
type TelemetryServiceV2 struct {
	agentpbv2.UnimplementedWendyTelemetryServiceServer
	logger      *zap.Logger
	broadcaster *TelemetryBroadcaster
	buffer      *TelemetryBuffer
}

// NewTelemetryServiceV2 creates a new TelemetryServiceV2.
func NewTelemetryServiceV2(logger *zap.Logger, broadcaster *TelemetryBroadcaster, buffer *TelemetryBuffer) *TelemetryServiceV2 {
	return &TelemetryServiceV2{logger: logger, broadcaster: broadcaster, buffer: buffer}
}

func (s *TelemetryServiceV2) StreamLogs(req *agentpbv2.StreamLogsRequest, stream grpc.ServerStreamingServer[agentpbv2.StreamLogsResponse]) error {
	if req.LastN != nil && *req.LastN > 0 && s.buffer != nil {
		entries := s.buffer.ReadLastN(SignalLogs, int(*req.LastN))
		for _, e := range entries {
			logs, ok := e.(*otelpb.ExportLogsServiceRequest)
			if !ok {
				continue
			}
			if err := stream.Send(&agentpbv2.StreamLogsResponse{Logs: logs, IsHistory: true}); err != nil {
				return err
			}
		}
	}

	subID, ch := s.broadcaster.SubscribeLogs()
	defer s.broadcaster.UnsubscribeLogs(subID)

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case item, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&agentpbv2.StreamLogsResponse{Logs: item}); err != nil {
				return err
			}
		}
	}
}

func (s *TelemetryServiceV2) StreamMetrics(req *agentpbv2.StreamMetricsRequest, stream grpc.ServerStreamingServer[agentpbv2.StreamMetricsResponse]) error {
	if req.LastN != nil && *req.LastN > 0 && s.buffer != nil {
		entries := s.buffer.ReadLastN(SignalMetrics, int(*req.LastN))
		for _, e := range entries {
			metrics, ok := e.(*otelpb.ExportMetricsServiceRequest)
			if !ok {
				continue
			}
			if err := stream.Send(&agentpbv2.StreamMetricsResponse{Metrics: metrics, IsHistory: true}); err != nil {
				return err
			}
		}
	}

	subID, ch := s.broadcaster.SubscribeMetrics()
	defer s.broadcaster.UnsubscribeMetrics(subID)

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case item, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&agentpbv2.StreamMetricsResponse{Metrics: item}); err != nil {
				return err
			}
		}
	}
}

func (s *TelemetryServiceV2) StreamTraces(req *agentpbv2.StreamTracesRequest, stream grpc.ServerStreamingServer[agentpbv2.StreamTracesResponse]) error {
	if req.LastN != nil && *req.LastN > 0 && s.buffer != nil {
		entries := s.buffer.ReadLastN(SignalTraces, int(*req.LastN))
		for _, e := range entries {
			traces, ok := e.(*otelpb.ExportTraceServiceRequest)
			if !ok {
				continue
			}
			if err := stream.Send(&agentpbv2.StreamTracesResponse{Traces: traces, IsHistory: true}); err != nil {
				return err
			}
		}
	}

	subID, ch := s.broadcaster.SubscribeTraces()
	defer s.broadcaster.UnsubscribeTraces(subID)

	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case item, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&agentpbv2.StreamTracesResponse{Traces: item}); err != nil {
				return err
			}
		}
	}
}
