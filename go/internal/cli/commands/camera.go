package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
	"github.com/wendylabsinc/wendy/internal/cli/tui"
	agentpb "github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

func newCameraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "camera",
		Short: "Manage cameras on the target device",
	}
	cmd.AddCommand(
		newCameraListCmd(),
		newCameraViewCmd(),
	)
	return cmd
}

func newCameraListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cameras",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := connectToAgent(ctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := conn.VideoService.ListVideoDevices(ctx, &agentpb.ListVideoDevicesRequest{})
			if err != nil {
				return fmt.Errorf("listing cameras: %w", err)
			}

			devices := resp.GetDevices()
			if jsonOutput {
				data, err := json.MarshalIndent(devices, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			if len(devices) == 0 {
				fmt.Println("No cameras found.")
				return nil
			}

			headers := []string{"ID", "Name", "Path"}
			var rows [][]string
			for _, d := range devices {
				rows = append(rows, []string{
					fmt.Sprintf("%d", d.GetId()),
					d.GetName(),
					d.GetPath(),
				})
			}
			fmt.Print(tui.RenderTable(headers, rows))
			return nil
		},
	}
}

func newCameraViewCmd() *cobra.Command {
	var deviceID, width, height, fps uint32
	var toStdout bool

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Stream H.264 video from a device camera",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := connectToAgent(ctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			stream, err := conn.VideoService.StreamVideo(ctx, &agentpb.StreamVideoRequest{
				DeviceId:  deviceID,
				Width:     width,
				Height:    height,
				Framerate: fps,
			})
			if err != nil {
				return fmt.Errorf("starting video stream: %w", err)
			}

			cliLogln("Streaming video (Ctrl+C to stop)...")

			if toStdout {
				return pipeVideoToStdout(stream, cmd.OutOrStdout())
			}
			return playVideoWithGStreamer(ctx, stream)
		},
	}

	cmd.Flags().Uint32Var(&deviceID, "id", 0, "Camera device ID")
	cmd.Flags().Uint32Var(&width, "width", 0, "Frame width (0 = device default)")
	cmd.Flags().Uint32Var(&height, "height", 0, "Frame height (0 = device default)")
	cmd.Flags().Uint32Var(&fps, "fps", 0, "Framerate (0 = device default)")
	cmd.Flags().BoolVar(&toStdout, "stdout", false, "Pipe encoded video to stdout instead of opening a window (codec: H.264 or VP8/WebM depending on device capabilities)")

	return cmd
}

// videoStream is the receive side of the StreamVideo gRPC stream.
type videoStream interface {
	Recv() (*agentpb.VideoFrame, error)
}

// pipeVideoToStdout writes VideoFrame data chunks to w until the stream ends.
func pipeVideoToStdout(stream videoStream, w io.Writer) error {
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("receiving video: %w", err)
		}
		if _, err := w.Write(frame.GetData()); err != nil {
			return fmt.Errorf("writing video data: %w", err)
		}
	}
}

// playVideoWithGStreamer spawns gst-launch-1.0 and feeds it the video stream via stdin.
// It peeks the first frame to determine the codec, then starts the matching decoder pipeline.
func playVideoWithGStreamer(ctx context.Context, stream videoStream) error {
	gstPath, err := resolveGSTLaunch()
	if err != nil {
		return err
	}

	// Peek the first frame to learn the codec.
	first, err := stream.Recv()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("receiving video: %w", err)
	}
	codec := first.GetCodec()

	gst := exec.CommandContext(ctx, gstPath, playbackPipelineArgs(codec)...)
	gst.Stderr = os.Stderr

	stdin, err := gst.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating GStreamer stdin pipe: %w", err)
	}

	if err := gst.Start(); err != nil {
		return fmt.Errorf("starting GStreamer: %w", err)
	}
	defer func() {
		stdin.Close()      //nolint:errcheck — signal EOF to GStreamer before killing
		gst.Process.Kill() //nolint:errcheck
		gst.Wait()         //nolint:errcheck
	}()

	done := make(chan error, 1)
	go func() { done <- feedGStreamer(ctx, stream, first, codec, stdin) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil
	}
}

// recvResult carries one stream.Recv outcome from the receive goroutine to the
// frame consumer.
type recvResult struct {
	frame *agentpb.VideoFrame
	err   error
}

// feedGStreamer writes the codec byte stream to GStreamer's stdin until the
// video stream ends. For H.264 it first drops the backlog that accumulated
// while gst-launch was starting, so playback begins near real time. A VP8/WebM
// stream cannot be joined mid-container, so its frames are written verbatim.
func feedGStreamer(ctx context.Context, stream videoStream, first *agentpb.VideoFrame, codec agentpb.VideoCodec, stdin io.Writer) error {
	if codec == agentpb.VideoCodec_VIDEO_CODEC_VP8 {
		if _, err := stdin.Write(first.GetData()); err != nil {
			return fmt.Errorf("writing to GStreamer: %w", err)
		}
		for {
			frame, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("receiving video: %w", err)
			}
			if _, err := stdin.Write(frame.GetData()); err != nil {
				return fmt.Errorf("writing to GStreamer: %w", err)
			}
		}
	}

	// H.264: a dedicated receive goroutine feeds a channel so the startup
	// drain can bound how long it waits for the next frame.
	frames := make(chan recvResult, 8)
	go func() {
		for {
			f, err := stream.Recv()
			select {
			case frames <- recvResult{frame: f, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	pending, err := drainStartupBacklogH264(ctx, frames, first)
	if err != nil {
		return err
	}
	if _, err := stdin.Write(pending); err != nil {
		return fmt.Errorf("writing to GStreamer: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-frames:
			if r.err == io.EOF {
				return nil
			}
			if r.err != nil {
				return fmt.Errorf("receiving video: %w", r.err)
			}
			if _, err := stdin.Write(r.frame.GetData()); err != nil {
				return fmt.Errorf("writing to GStreamer: %w", err)
			}
		}
	}
}

// startupBacklogGap bounds how long the H.264 startup drain waits for the next
// frame before concluding the spawn-time backlog is exhausted. It must exceed
// the sub-millisecond inter-arrival of buffered backlog frames and stay below
// the real-time inter-frame interval at 60fps (~16.7ms). Declared as a var so
// tests can shorten it.
var startupBacklogGap = 12 * time.Millisecond

// drainStartupBacklogH264 consumes the H.264 frames that piled up while
// gst-launch was starting and returns the bytes to feed the decoder first:
// everything from the most recent keyframe onward. Backlog frames arrive in a
// tight burst and are dropped up to that keyframe; the drain ends once a frame
// takes longer than startupBacklogGap to arrive (the camera's real cadence) or
// the stream ends. Ending early or late is graceful — the result always begins
// at a keyframe, and the decoder's leaky queue drops any remaining backlog.
func drainStartupBacklogH264(ctx context.Context, frames <-chan recvResult, first *agentpb.VideoFrame) ([]byte, error) {
	var pending []byte
	keep := func(data []byte) {
		if off, ok := lastKeyframeOffset(data); ok {
			pending = append(pending[:0], data[off:]...)
		} else {
			pending = append(pending, data...)
		}
	}
	keep(first.GetData())

	timer := time.NewTimer(startupBacklogGap)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return pending, nil
		case <-timer.C:
			return pending, nil
		case r := <-frames:
			if r.err == io.EOF {
				return pending, nil
			}
			if r.err != nil {
				return nil, fmt.Errorf("receiving video: %w", r.err)
			}
			keep(r.frame.GetData())
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(startupBacklogGap)
		}
	}
}

// H.264 NAL unit types relevant to keyframe detection (ITU-T H.264 Table 7-1).
const (
	h264NalTypeIDR = 5 // coded slice of an IDR picture
	h264NalTypeSPS = 7 // sequence parameter set
)

// nextStartCode returns the index of the next Annex-B start code (00 00 01,
// with an immediately preceding 00 absorbed so a 4-byte code is reported whole)
// at or after from, together with the index of the NAL header byte that
// follows it. found is false when no start code remains.
func nextStartCode(data []byte, from int) (codeStart, headerIdx int, found bool) {
	for i := from; i+2 < len(data); i++ {
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			cs := i
			if cs > 0 && data[cs-1] == 0x00 {
				cs--
			}
			return cs, i + 3, true
		}
	}
	return 0, 0, false
}

// lastKeyframeOffset scans Annex-B H.264 data and returns the byte offset of
// the start code that begins the most recent keyframe — the SPS preceding an
// IDR slice (the agent repeats SPS/PPS before every keyframe via h264parse
// config-interval=-1), or the IDR's own start code when no SPS precedes it in
// this data. found is false when data contains no keyframe.
func lastKeyframeOffset(data []byte) (offset int, found bool) {
	spsStart := -1
	for i := 0; ; {
		codeStart, headerIdx, ok := nextStartCode(data, i)
		if !ok || headerIdx >= len(data) {
			break
		}
		switch data[headerIdx] & 0x1F {
		case h264NalTypeSPS:
			spsStart = codeStart
		case h264NalTypeIDR:
			if spsStart >= 0 {
				offset = spsStart
			} else {
				offset = codeStart
			}
			found = true
			spsStart = -1
		}
		i = headerIdx
	}
	return offset, found
}

// playbackPipelineArgs returns the gst-launch-1.0 element arguments for decoding
// and displaying the incoming stream of the given codec, read from stdin (fd 0).
func playbackPipelineArgs(codec agentpb.VideoCodec) []string {
	switch codec {
	case agentpb.VideoCodec_VIDEO_CODEC_VP8:
		// Server sends VP8 in a WebM container (webmmux streamable=true).
		// The leaky queue after matroskademux drops whole frames when decode
		// falls behind, draining an encoded-side backlog instead of playing
		// through it; the queue after the decoder absorbs display-sink jitter.
		return []string{
			"fdsrc", "fd=0",
			"!", "matroskademux",
			"!", "queue", "max-size-buffers=2", "leaky=downstream",
			"!", "vp8dec",
			"!", "videoconvert",
			"!", "queue", "max-size-buffers=1", "leaky=downstream",
			"!", "autovideosink", "sync=false",
		}
	default: // H264
		// fdsrc emits untyped buffers (no caps); h264parse needs video/x-h264.
		// A bare "video/x-h264" capsfilter here cannot bridge that gap: the
		// capsfilter must fixate caps onto the untyped buffers, but video/x-h264
		// alone is unfixed (width/height/framerate are template ranges), so it
		// fails with "Output caps are unfixed" and the pipeline won't preroll.
		// typefind inspects the actual bytes, detects the H.264 start codes, and
		// sets fixed content-derived caps; h264parse then auto-detects whether
		// the stream is Annex B byte-stream or length-prefixed AVC.
		//
		// The leaky queue between h264parse and the decoder drops whole access
		// units when decode cannot keep up, so an encoded-side backlog drains
		// by dropping rather than playing through. max-threads=1 removes
		// avdec_h264's frame-threading output delay (~thread-count frames).
		return []string{
			"fdsrc", "fd=0",
			"!", "typefind",
			"!", "h264parse",
			"!", "queue", "max-size-buffers=2", "leaky=downstream",
			"!", "avdec_h264", "max-threads=1",
			"!", "videoconvert",
			"!", "queue", "max-size-buffers=1", "leaky=downstream",
			"!", "autovideosink", "sync=false",
		}
	}
}
