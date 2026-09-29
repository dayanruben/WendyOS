package services

import (
	"bytes"
	"context"
	"testing"

	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
)

func TestWebMInitializationAcrossChunkBoundaries(t *testing.T) {
	// EBML header, unknown-size Segment, metadata containing a false Cluster ID,
	// then the real Cluster. IDs inside metadata must not cut the header short.
	header := []byte{0x1a, 0x45, 0xdf, 0xa3, 0x80, 0x18, 0x53, 0x80, 0x67, 0xff, 0xec, 0x84, 0x1f, 0x43, 0xb6, 0x75}
	stream := append(append([]byte(nil), header...), 0x1f, 0x43, 0xb6, 0x75, 0x80)
	for chunk := 1; chunk <= len(stream); chunk++ {
		var init webmInitialization
		for off := 0; off < len(stream); off += chunk {
			init.feed(stream[off:min(off+chunk, len(stream))])
		}
		if !bytes.Equal(init.header, header) {
			t.Fatalf("chunk %d: header %x, want %x", chunk, init.header, header)
		}
		init.feed([]byte("later cluster payload"))
		if !bytes.Equal(init.header, header) || init.pending != nil {
			t.Fatal("initialization grew after first Cluster")
		}
	}
}

func TestWebMInitializationBoundedAndRejectsInvalidPrefix(t *testing.T) {
	for _, input := range [][]byte{bytes.Repeat([]byte{0}, maxWebMInitialization+100), {0x1a, 0x45, 0xdf, 0xa3, 0xff}, {0x81, 0x80}} {
		var init webmInitialization
		init.feed(input)
		if !init.failed || len(init.pending) != 0 || len(init.header) != 0 {
			t.Fatal("invalid initialization retained")
		}
	}
}

func TestWebMLateSubscriberReceivesInitializationWithoutChangingSample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := &deviceHub{ctx: ctx, subs: make(map[int]*hubSubscriber)}
	_, _, err := hub.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	header := []byte{0x1a, 0x45, 0xdf, 0xa3, 0x80, 0x18, 0x53, 0x80, 0x67, 0xff}
	hub.broadcast(&videoFrame{codec: agentpb.VideoCodec_VIDEO_CODEC_VP8, data: append(append([]byte(nil), header...), 0x1f, 0x43, 0xb6, 0x75, 0x80)})
	id, ch, err := hub.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("mid-stream encoded payload")
	hub.broadcast(&videoFrame{codec: agentpb.VideoCodec_VIDEO_CODEC_VP8, data: payload, sampleID: 91})
	sub := cameraSensorSubscription{hub: hub, subID: id, frames: ch}
	sample, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sample.SampleID != 91 || !bytes.Equal(sample.Payload, payload) || !bytes.Equal(sample.DecoderInit, header) {
		t.Fatalf("sample identity or initialization lost: %+v", sample)
	}
}
