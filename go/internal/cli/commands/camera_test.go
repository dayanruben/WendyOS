package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentpb "github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

// Annex-B NAL header bytes (forbidden_zero | nal_ref_idc | nal_unit_type).
const (
	nalHdrSPS = 0x67 // sequence parameter set (type 7)
	nalHdrPPS = 0x68 // picture parameter set (type 8)
	nalHdrIDR = 0x65 // IDR slice (type 5) — a keyframe
	nalHdrP   = 0x41 // non-IDR slice (type 1)
)

// h264NAL builds an Annex-B NAL unit: a 4-byte start code, a header byte, then payload.
func h264NAL(header byte, payload ...byte) []byte {
	return append([]byte{0x00, 0x00, 0x00, 0x01, header}, payload...)
}

// joinBytes concatenates byte slices.
func joinBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type mockVideoStream struct {
	frames []*agentpb.VideoFrame
	idx    int
}

func (m *mockVideoStream) Recv() (*agentpb.VideoFrame, error) {
	if m.idx >= len(m.frames) {
		return nil, io.EOF
	}
	f := m.frames[m.idx]
	m.idx++
	return f, nil
}

func TestPipeVideoToStdout_WritesAllFrames(t *testing.T) {
	stream := &mockVideoStream{
		frames: []*agentpb.VideoFrame{
			{Data: []byte{0x00, 0x00, 0x00, 0x01}},
			{Data: []byte{0x41, 0x42, 0x43}},
		},
	}
	var buf bytes.Buffer
	if err := pipeVideoToStdout(stream, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x42, 0x43}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("got %v, want %v", buf.Bytes(), expected)
	}
}

func TestPipeVideoToStdout_EmptyStream(t *testing.T) {
	stream := &mockVideoStream{}
	var buf bytes.Buffer
	if err := pipeVideoToStdout(stream, &buf); err != nil {
		t.Fatalf("unexpected error for empty stream: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got %d bytes", buf.Len())
	}
}

func TestPlaybackPipelineArgs_H264UsesTypefindNotBareCaps(t *testing.T) {
	args := playbackPipelineArgs(agentpb.VideoCodec_VIDEO_CODEC_H264)
	joined := strings.Join(args, " ")

	// Regression: a bare "video/x-h264" capsfilter directly after fdsrc cannot
	// fixate caps onto fdsrc's untyped buffers and fails to preroll with
	// "Output caps are unfixed". typefind must classify the stream instead.
	if !strings.Contains(joined, "fdsrc fd=0 ! typefind ! h264parse") {
		t.Errorf("H264 pipeline must route fdsrc through typefind into h264parse, got: %v", args)
	}
	if strings.Contains(joined, "! video/x-h264 !") {
		t.Errorf("H264 pipeline must not use a bare video/x-h264 capsfilter after fdsrc, got: %v", args)
	}
}

func TestPlaybackPipelineArgs_VP8UsesMatroskademux(t *testing.T) {
	args := playbackPipelineArgs(agentpb.VideoCodec_VIDEO_CODEC_VP8)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "fdsrc fd=0 ! matroskademux") {
		t.Errorf("VP8 pipeline must demux the WebM container via matroskademux, got: %v", args)
	}
	if !strings.Contains(joined, "! vp8dec !") {
		t.Errorf("VP8 pipeline must decode via vp8dec, got: %v", args)
	}
}

func TestPlaybackPipelineArgs_H264DecodesSingleThreaded(t *testing.T) {
	args := playbackPipelineArgs(agentpb.VideoCodec_VIDEO_CODEC_H264)
	joined := strings.Join(args, " ")
	// Frame-based multithreading in avdec_h264 delays output by ~thread-count
	// frames; max-threads=1 removes that constant latency.
	if !strings.Contains(joined, "avdec_h264 max-threads=1") {
		t.Errorf("H264 pipeline must decode single-threaded to avoid frame-threading latency, got: %v", args)
	}
}

func TestPlaybackPipelineArgs_H264LeakyQueueBeforeDecoder(t *testing.T) {
	args := playbackPipelineArgs(agentpb.VideoCodec_VIDEO_CODEC_H264)
	joined := strings.Join(args, " ")
	// A leaky queue between h264parse and the decoder drops whole access units
	// when decode falls behind, so an encoded-side backlog drains by dropping
	// rather than playing through.
	if !strings.Contains(joined, "h264parse ! queue max-size-buffers=2 leaky=downstream ! avdec_h264") {
		t.Errorf("H264 pipeline must have a leaky queue before the decoder, got: %v", args)
	}
}

func TestPlaybackPipelineArgs_VP8LeakyQueueBeforeDecoder(t *testing.T) {
	args := playbackPipelineArgs(agentpb.VideoCodec_VIDEO_CODEC_VP8)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "matroskademux ! queue max-size-buffers=2 leaky=downstream ! vp8dec") {
		t.Errorf("VP8 pipeline must have a leaky queue before the decoder, got: %v", args)
	}
}

func TestPlayVideoWithGStreamer_MissingGStreamer(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir — no executables on PATH
	stubGSTFallback(t, nil)       // no install-location fallbacks either

	stream := &mockVideoStream{}
	err := playVideoWithGStreamer(context.Background(), stream)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

// stubGSTFallback overrides the platform-specific fallback path list for the
// duration of the test.
func stubGSTFallback(t *testing.T, paths []string) {
	t.Helper()
	prev := gstLaunchFallbackPathsFn
	gstLaunchFallbackPathsFn = func() []string { return paths }
	t.Cleanup(func() { gstLaunchFallbackPathsFn = prev })
}

func TestResolveGSTLaunch_FoundViaFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // not on PATH

	dir := t.TempDir()
	bin := filepath.Join(dir, gstLaunchName)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing fake binary: %v", err)
	}
	stubGSTFallback(t, []string{filepath.Join(dir, "missing"), bin})

	got, err := resolveGSTLaunch()
	if err != nil {
		t.Fatalf("expected to resolve via fallback, got error: %v", err)
	}
	if got != bin {
		t.Errorf("got %q, want %q", got, bin)
	}
}

func TestResolveGSTLaunch_IgnoresDirectories(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	dir := t.TempDir()
	// A directory named like the binary must not be treated as a match.
	if err := os.Mkdir(filepath.Join(dir, gstLaunchName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stubGSTFallback(t, []string{filepath.Join(dir, gstLaunchName)})

	if _, err := resolveGSTLaunch(); err == nil {
		t.Fatal("expected error when only a directory matches, got nil")
	}
}

func TestLastKeyframeOffset_NoKeyframe(t *testing.T) {
	data := joinBytes(h264NAL(nalHdrP, 0xAA), h264NAL(nalHdrP, 0xBB))
	if off, ok := lastKeyframeOffset(data); ok {
		t.Errorf("expected no keyframe in a P-slice-only stream, got offset %d", off)
	}
}

func TestLastKeyframeOffset_SPSBeforeIDR(t *testing.T) {
	data := joinBytes(h264NAL(nalHdrSPS, 0x01), h264NAL(nalHdrPPS, 0x02), h264NAL(nalHdrIDR, 0x03))
	off, ok := lastKeyframeOffset(data)
	if !ok || off != 0 {
		t.Errorf("keyframe should start at the SPS (offset 0), got off=%d ok=%v", off, ok)
	}
}

func TestLastKeyframeOffset_PicksMostRecent(t *testing.T) {
	leading := joinBytes(h264NAL(nalHdrSPS), h264NAL(nalHdrIDR), h264NAL(nalHdrP))
	data := joinBytes(leading, h264NAL(nalHdrSPS), h264NAL(nalHdrPPS), h264NAL(nalHdrIDR))
	off, ok := lastKeyframeOffset(data)
	if !ok || off != len(leading) {
		t.Errorf("expected the most recent keyframe at offset %d, got off=%d ok=%v", len(leading), off, ok)
	}
}

func TestLastKeyframeOffset_IDRWithoutSPS(t *testing.T) {
	leading := h264NAL(nalHdrP)
	data := joinBytes(leading, h264NAL(nalHdrIDR))
	off, ok := lastKeyframeOffset(data)
	if !ok || off != len(leading) {
		t.Errorf("a bare IDR should be reported at its own start code (offset %d), got off=%d ok=%v", len(leading), off, ok)
	}
}

func TestLastKeyframeOffset_ThreeByteStartCode(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, nalHdrSPS, 0x00, 0x00, 0x01, nalHdrIDR}
	off, ok := lastKeyframeOffset(data)
	if !ok || off != 0 {
		t.Errorf("expected keyframe at offset 0 with 3-byte start codes, got off=%d ok=%v", off, ok)
	}
}

func TestLastKeyframeOffset_Empty(t *testing.T) {
	if _, ok := lastKeyframeOffset(nil); ok {
		t.Error("expected no keyframe in empty data")
	}
}

func TestDrainStartupBacklogH264_SkipsToMostRecentKeyframe(t *testing.T) {
	idr1 := joinBytes(h264NAL(nalHdrSPS), h264NAL(nalHdrIDR, 0x11))
	p1 := h264NAL(nalHdrP, 0x22)
	idr2 := joinBytes(h264NAL(nalHdrSPS), h264NAL(nalHdrIDR, 0x33))
	p2 := h264NAL(nalHdrP, 0x44)

	frames := make(chan recvResult, 4)
	frames <- recvResult{frame: &agentpb.VideoFrame{Data: p1}}
	frames <- recvResult{frame: &agentpb.VideoFrame{Data: idr2}}
	frames <- recvResult{frame: &agentpb.VideoFrame{Data: p2}}
	frames <- recvResult{err: io.EOF}

	pending, err := drainStartupBacklogH264(context.Background(), frames, &agentpb.VideoFrame{Data: idr1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := joinBytes(idr2, p2)
	if !bytes.Equal(pending, want) {
		t.Errorf("drain kept %v, want %v (most recent keyframe onward)", pending, want)
	}
}

func TestDrainStartupBacklogH264_NoBacklogKeepsFirstFrame(t *testing.T) {
	idr := joinBytes(h264NAL(nalHdrSPS), h264NAL(nalHdrIDR, 0x11))
	frames := make(chan recvResult) // nothing arrives — drain ends on the idle gap

	pending, err := drainStartupBacklogH264(context.Background(), frames, &agentpb.VideoFrame{Data: idr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(pending, idr) {
		t.Errorf("drain kept %v, want the first frame %v", pending, idr)
	}
}

func TestDrainStartupBacklogH264_StreamError(t *testing.T) {
	idr := joinBytes(h264NAL(nalHdrSPS), h264NAL(nalHdrIDR))
	frames := make(chan recvResult, 1)
	frames <- recvResult{err: errors.New("boom")}

	if _, err := drainStartupBacklogH264(context.Background(), frames, &agentpb.VideoFrame{Data: idr}); err == nil {
		t.Fatal("expected the stream error to propagate, got nil")
	}
}
