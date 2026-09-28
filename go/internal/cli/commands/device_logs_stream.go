package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

// logStreamReceiver is the part of a StreamLogs client stream that
// consumeLogStream reads. Tests substitute a scripted fake.
type logStreamReceiver interface {
	Recv() (*agentpb.StreamLogsResponse, error)
}

// How `wendy device logs --no-follow` decides the agent's history replay is
// over. Vars so tests can shrink them.
//
// The protocol has no end-of-history marker, and agents already in the field
// can't grow one. What every agent does do is send its whole replay first —
// the on-disk batches for --tail, then its in-memory recent batches — all
// back-to-back and flagged IsHistory, before entering the live loop, whose
// frames (new logs, and since WDY-2912 an empty heartbeat after 15 s of
// quiet) are never flagged. So:
//
//   - the first frame without IsHistory proves the replay is over; and
//   - a pause of noFollowIdleGap after a replayed frame is taken to mean the
//     same. The replay is a single burst (sub-millisecond gaps observed from a
//     Jetson over USB), so this only cuts history short on a link that stalls
//     for that long mid-burst. That is the trade-off for not waiting up to
//     15 s for a heartbeat that pre-WDY-2912 agents never send.
//
// noFollowFirstFrameWait covers the agent reading its on-disk buffer before
// the first replayed frame (it scans segments newest-first until it has N
// matches, which can take seconds for a quiet app on a large buffer). When
// there is no history at all, it is how long --no-follow waits to conclude so.
var (
	noFollowFirstFrameWait = 10 * time.Second
	noFollowIdleGap        = 1500 * time.Millisecond
)

// consumeLogStream reads StreamLogs frames and passes each one to handle.
//
// With follow it runs until the stream ends. Without follow it returns once
// the history replay is over (see noFollowIdleGap) and only hands replayed
// frames to handle. In both modes a cancelled ctx — Ctrl-C — is a clean exit
// rather than a "receiving logs: ... Canceled" error.
//
// ctx must be the context the stream was opened with, and the caller must
// cancel it after consumeLogStream returns: that is what unblocks the
// background Recv on the !follow path.
func consumeLogStream(ctx context.Context, stream logStreamReceiver, follow bool, handle func(*agentpb.StreamLogsResponse)) error {
	if follow {
		for {
			resp, err := stream.Recv()
			if err != nil {
				return logStreamEndErr(ctx, err)
			}
			handle(resp)
		}
	}

	type frame struct {
		resp *agentpb.StreamLogsResponse
		err  error
	}
	frames := make(chan frame)
	go func() {
		for {
			resp, err := stream.Recv()
			select {
			case frames <- frame{resp, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	idle := time.NewTimer(noFollowFirstFrameWait)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-idle.C:
			return nil
		case f := <-frames:
			if f.err != nil {
				return logStreamEndErr(ctx, f.err)
			}
			if !f.resp.GetIsHistory() {
				return nil
			}
			handle(f.resp)
			idle.Reset(noFollowIdleGap)
		}
	}
}

// logStreamEndErr maps the error that ended a log stream to the command's
// result. The agent closing the stream (EOF) and the user interrupting it
// (ctx cancelled) are normal ends; anything else is a real failure.
func logStreamEndErr(ctx context.Context, err error) error {
	if errors.Is(err, io.EOF) || ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("receiving logs: %w", err)
}
