package commands

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeLogStream replays scripted frames the way a gRPC client stream does:
// frames in order, then endErr (io.EOF when nil) once frames is closed. Recv
// blocks while nothing is queued, and returns the gRPC Canceled error when
// ctx is cancelled — exactly what a real stream returns on Ctrl-C.
type fakeLogStream struct {
	ctx    context.Context
	frames chan *agentpb.StreamLogsResponse
	endErr error
}

func newFakeLogStream(ctx context.Context, frames ...*agentpb.StreamLogsResponse) *fakeLogStream {
	f := &fakeLogStream{ctx: ctx, frames: make(chan *agentpb.StreamLogsResponse, len(frames)+1)}
	for _, fr := range frames {
		f.frames <- fr
	}
	return f
}

func (f *fakeLogStream) Recv() (*agentpb.StreamLogsResponse, error) {
	select {
	case r, ok := <-f.frames:
		if !ok {
			if f.endErr != nil {
				return nil, f.endErr
			}
			return nil, io.EOF
		}
		return r, nil
	case <-f.ctx.Done():
		return nil, status.FromContextError(f.ctx.Err()).Err()
	}
}

func logsFrame(history bool, body string) *agentpb.StreamLogsResponse {
	return &agentpb.StreamLogsResponse{
		IsHistory: history,
		Logs: &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: []*logspb.LogRecord{{
				Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: body}},
			}}}},
		}}},
	}
}

func historyFrame(body string) *agentpb.StreamLogsResponse { return logsFrame(true, body) }
func liveFrame(body string) *agentpb.StreamLogsResponse    { return logsFrame(false, body) }

// heartbeatFrame is the empty frame agents send after 15 s of quiet (WDY-2912).
func heartbeatFrame() *agentpb.StreamLogsResponse { return &agentpb.StreamLogsResponse{} }

// frameRecorder collects the bodies of the frames consumeLogStream handles.
type frameRecorder struct {
	mu     sync.Mutex
	bodies []string
}

func (r *frameRecorder) handle(resp *agentpb.StreamLogsResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rl := range resp.GetLogs().GetResourceLogs() {
		for _, sl := range rl.GetScopeLogs() {
			for _, lr := range sl.GetLogRecords() {
				r.bodies = append(r.bodies, lr.GetBody().GetStringValue())
			}
		}
	}
}

func (r *frameRecorder) got() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.bodies, ",")
}

// setNoFollowTimings shrinks or stretches the --no-follow timers for one test.
func setNoFollowTimings(t *testing.T, firstFrame, idleGap time.Duration) {
	t.Helper()
	prevFirst, prevIdle := noFollowFirstFrameWait, noFollowIdleGap
	noFollowFirstFrameWait, noFollowIdleGap = firstFrame, idleGap
	t.Cleanup(func() { noFollowFirstFrameWait, noFollowIdleGap = prevFirst, prevIdle })
}

// runNoFollow runs consumeLogStream without follow and cancels the stream
// context afterwards, as the command does.
func runNoFollow(t *testing.T, frames ...*agentpb.StreamLogsResponse) (string, time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rec frameRecorder
	start := time.Now()
	err := consumeLogStream(ctx, newFakeLogStream(ctx, frames...), false, rec.handle)
	return rec.got(), time.Since(start), err
}

func TestConsumeLogStream_NoFollowStopsAtFirstLiveFrame(t *testing.T) {
	// Idle timers far longer than the test: only the live frame can end it.
	setNoFollowTimings(t, time.Minute, time.Minute)
	got, elapsed, err := runNoFollow(t, historyFrame("h1"), historyFrame("h2"), liveFrame("live"), historyFrame("never"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "h1,h2" {
		t.Fatalf("handled %q, want only the replayed frames h1,h2", got)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %s; the live frame should end the replay immediately", elapsed)
	}
}

func TestConsumeLogStream_NoFollowStopsAtHeartbeat(t *testing.T) {
	setNoFollowTimings(t, time.Minute, time.Minute)
	got, _, err := runNoFollow(t, historyFrame("h1"), heartbeatFrame())
	if err != nil || got != "h1" {
		t.Fatalf("got %q, err %v; want h1 and a clean stop at the heartbeat", got, err)
	}
}

func TestConsumeLogStream_NoFollowStopsWhenReplayGoesQuiet(t *testing.T) {
	// Pre-WDY-2912 agents send no heartbeat: after the burst, nothing arrives.
	setNoFollowTimings(t, time.Minute, 50*time.Millisecond)
	got, elapsed, err := runNoFollow(t, historyFrame("h1"), historyFrame("h2"))
	if err != nil || got != "h1,h2" {
		t.Fatalf("got %q, err %v; want h1,h2 and a clean stop", got, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %s; want to stop after the idle gap", elapsed)
	}
}

func TestConsumeLogStream_NoFollowWithoutHistoryGivesUpAfterFirstFrameWait(t *testing.T) {
	setNoFollowTimings(t, 50*time.Millisecond, time.Minute)
	got, elapsed, err := runNoFollow(t)
	if err != nil || got != "" {
		t.Fatalf("got %q, err %v; want nothing and a clean stop", got, err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took %s; want to give up after noFollowFirstFrameWait", elapsed)
	}
}

func TestConsumeLogStream_NoFollowReportsStreamErrors(t *testing.T) {
	setNoFollowTimings(t, time.Minute, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newFakeLogStream(ctx, historyFrame("h1"))
	stream.endErr = status.Error(codes.Unavailable, "agent went away")
	close(stream.frames)
	var rec frameRecorder
	err := consumeLogStream(ctx, stream, false, rec.handle)
	if err == nil || !strings.Contains(err.Error(), "receiving logs") {
		t.Fatalf("err = %v, want the stream failure reported", err)
	}
}

// Ctrl-C while following used to end with "receiving logs: rpc error: code =
// Canceled desc = context canceled" and exit status 1.
func TestConsumeLogStream_FollowCancelIsCleanExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rec frameRecorder
	handled := 0
	err := consumeLogStream(ctx, newFakeLogStream(ctx, historyFrame("h1"), liveFrame("l1")), true, func(resp *agentpb.StreamLogsResponse) {
		rec.handle(resp)
		if handled++; handled == 2 {
			cancel() // the user presses Ctrl-C while following
		}
	})
	if err != nil {
		t.Fatalf("err = %v, want a clean exit on cancel", err)
	}
	if got := rec.got(); got != "h1,l1" {
		t.Fatalf("handled %q, want h1,l1 (follow mode passes live frames through)", got)
	}
}

func TestConsumeLogStream_FollowReportsStreamErrorsAndEOF(t *testing.T) {
	ctx := context.Background()

	eof := newFakeLogStream(ctx, liveFrame("l1"))
	close(eof.frames)
	if err := consumeLogStream(ctx, eof, true, func(*agentpb.StreamLogsResponse) {}); err != nil {
		t.Fatalf("EOF: err = %v, want nil", err)
	}

	broken := newFakeLogStream(ctx)
	broken.endErr = status.Error(codes.Unavailable, "agent went away")
	close(broken.frames)
	err := consumeLogStream(ctx, broken, true, func(*agentpb.StreamLogsResponse) {})
	if err == nil || !strings.Contains(err.Error(), "receiving logs") || !strings.Contains(err.Error(), "agent went away") {
		t.Fatalf("err = %v, want the stream failure reported", err)
	}
}

func TestDeviceLogsHasNoFollowFlag(t *testing.T) {
	f := newDeviceLogsCmd().Flags().Lookup("no-follow")
	if f == nil {
		t.Fatal("device logs is missing --no-follow")
	}
	if f.DefValue != "false" {
		t.Fatalf("--no-follow default = %q, want false (following stays the default)", f.DefValue)
	}
}
