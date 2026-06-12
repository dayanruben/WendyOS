package services

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

// fakeROS2Runtime scripts ExecROS2 responses keyed by the joined args.
type fakeROS2Runtime struct {
	sidecar   ROS2Sidecar
	ensureErr error
	// outputs maps "node list" → stdout. Missing keys exit 1.
	outputs map[string]string
	// execFn, when set, overrides the outputs map entirely.
	execFn func(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error)
	calls  []ROS2ExecOptions
}

func (f *fakeROS2Runtime) FindROS2Containers(context.Context) ([]ROS2Target, error) {
	return nil, nil
}

func (f *fakeROS2Runtime) EnsureROS2Sidecar(context.Context) (ROS2Sidecar, error) {
	if f.ensureErr != nil {
		return ROS2Sidecar{}, f.ensureErr
	}
	return f.sidecar, nil
}

func (f *fakeROS2Runtime) StopROS2Sidecar(context.Context) error { return nil }

func (f *fakeROS2Runtime) ExecROS2(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error) {
	f.calls = append(f.calls, opts)
	if f.execFn != nil {
		return f.execFn(ctx, opts, stdout, stderr)
	}
	key := strings.Join(opts.Args, " ")
	out, ok := f.outputs[key]
	if !ok {
		fmt.Fprintf(stderr, "unknown command: ros2 %s", key)
		return 1, nil
	}
	_, _ = io.WriteString(stdout, out)
	return 0, nil
}

func newTestROS2Service(t *testing.T, runtime ROS2Runtime, bagDir string) *ROS2Service {
	t.Helper()
	return NewROS2Service(zaptest.NewLogger(t), runtime, bagDir)
}

func TestROS2Service_ListNodes(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble", DomainID: 7},
		outputs: map[string]string{"node list": "/talker\n/camera/driver\n"},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	resp, err := svc.ListNodes(context.Background(), &agentpbv2.ListROS2NodesRequest{})
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(resp.GetNodes()) != 2 {
		t.Fatalf("got %d nodes, want 2", len(resp.GetNodes()))
	}
	if rt.calls[0].DomainID != 7 {
		t.Errorf("exec domain = %d, want sidecar default 7", rt.calls[0].DomainID)
	}
}

func TestROS2Service_DomainOverride(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble", DomainID: 7},
		outputs: map[string]string{"node list": ""},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	override := int32(42)
	if _, err := svc.ListNodes(context.Background(), &agentpbv2.ListROS2NodesRequest{DomainId: &override}); err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if rt.calls[0].DomainID != 42 {
		t.Errorf("exec domain = %d, want override 42", rt.calls[0].DomainID)
	}

	bad := int32(200)
	_, err := svc.ListNodes(context.Background(), &agentpbv2.ListROS2NodesRequest{DomainId: &bad})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("out-of-range override error = %v, want InvalidArgument", err)
	}
}

func TestROS2Service_NoSidecarIsFailedPrecondition(t *testing.T) {
	rt := &fakeROS2Runtime{ensureErr: errors.New("no running ROS 2 containers found")}
	svc := newTestROS2Service(t, rt, t.TempDir())
	_, err := svc.ListNodes(context.Background(), &agentpbv2.ListROS2NodesRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("error = %v, want FailedPrecondition", err)
	}
}

func TestROS2Service_ListTopicsWithCounts(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		outputs: map[string]string{
			"topic list -t":    "/scan [sensor_msgs/msg/LaserScan]\n",
			"topic info /scan": "Type: sensor_msgs/msg/LaserScan\nPublisher count: 1\nSubscription count: 3\n",
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	resp, err := svc.ListTopics(context.Background(), &agentpbv2.ListROS2TopicsRequest{IncludeCounts: true})
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(resp.GetTopics()) != 1 {
		t.Fatalf("got %d topics", len(resp.GetTopics()))
	}
	topic := resp.GetTopics()[0]
	if topic.GetPublisherCount() != 1 || topic.GetSubscriberCount() != 3 {
		t.Errorf("counts = %d/%d, want 1/3", topic.GetPublisherCount(), topic.GetSubscriberCount())
	}
}

func TestROS2Service_GetSetParam(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		outputs: map[string]string{
			"param get /talker use_sim_time":      "Boolean value is: False\n",
			"param set /talker use_sim_time true": "Set parameter successful\n",
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	got, err := svc.GetParam(context.Background(), &agentpbv2.GetROS2ParamRequest{Node: "/talker", Param: "use_sim_time"})
	if err != nil {
		t.Fatalf("GetParam: %v", err)
	}
	if got.GetValue() != "Boolean value is: False" {
		t.Errorf("value = %q", got.GetValue())
	}
	set, err := svc.SetParam(context.Background(), &agentpbv2.SetROS2ParamRequest{Node: "/talker", Param: "use_sim_time", Value: "true"})
	if err != nil {
		t.Fatalf("SetParam: %v", err)
	}
	if !set.GetSuccess() {
		t.Errorf("SetParam success = false: %s", set.GetMessage())
	}

	// Injection attempts must be rejected before reaching the runtime.
	if _, err := svc.GetParam(context.Background(), &agentpbv2.GetROS2ParamRequest{Node: "/talker; reboot", Param: "x"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("crafted node name error = %v, want InvalidArgument", err)
	}
}

func TestROS2Service_GetGraph(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		outputs: map[string]string{
			"node list": "/lidar\n/slam\n",
			"node info /lidar": `/lidar
  Subscribers:
  Publishers:
    /scan: sensor_msgs/msg/LaserScan
`,
			"node info /slam": `/slam
  Subscribers:
    /scan: sensor_msgs/msg/LaserScan
  Publishers:
    /map: nav_msgs/msg/OccupancyGrid
`,
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	resp, err := svc.GetGraph(context.Background(), &agentpbv2.GetROS2GraphRequest{})
	if err != nil {
		t.Fatalf("GetGraph: %v", err)
	}
	if len(resp.GetNodes()) != 2 || len(resp.GetPublishes()) != 2 || len(resp.GetSubscribes()) != 1 {
		t.Errorf("graph = %d nodes, %d pubs, %d subs; want 2/2/1", len(resp.GetNodes()), len(resp.GetPublishes()), len(resp.GetSubscribes()))
	}
	if resp.GetSubscribes()[0].GetNode() != "/slam" || resp.GetSubscribes()[0].GetTopic() != "/scan" {
		t.Errorf("subscribe edge = %+v", resp.GetSubscribes()[0])
	}
}

// fakeServerStream implements grpc.ServerStreamingServer[T] for tests.
type fakeServerStream[T any] struct {
	grpc.ServerStream
	ctx  context.Context
	sent []*T
}

func (f *fakeServerStream[T]) Send(msg *T) error {
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeServerStream[T]) Context() context.Context { return f.ctx }

func TestROS2Service_EchoTopic_CountLimit(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		execFn: func(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error) {
			for i := 0; ; i++ {
				if ctx.Err() != nil {
					return 130, ctx.Err()
				}
				if i > 100 {
					return 0, nil // safety: fake publisher gives up eventually
				}
				fmt.Fprintf(stdout, "data: msg-%d\n---\n", i)
			}
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	stream := &fakeServerStream[agentpbv2.ROS2Message]{ctx: context.Background()}
	err := svc.EchoTopic(&agentpbv2.EchoROS2TopicRequest{Topic: "/chatter", Count: 3}, stream)
	if err != nil {
		t.Fatalf("EchoTopic: %v", err)
	}
	if len(stream.sent) != 3 {
		t.Fatalf("got %d messages, want 3", len(stream.sent))
	}
	if !strings.Contains(stream.sent[0].GetYaml(), "msg-0") {
		t.Errorf("first message = %q", stream.sent[0].GetYaml())
	}
}

func TestROS2Service_MonitorHz(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		execFn: func(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error) {
			io.WriteString(stdout, "average rate: 30.001\n")
			io.WriteString(stdout, "\tmin: 0.032s max: 0.034s std dev: 0.00041s window: 31\n")
			return 0, nil
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	stream := &fakeServerStream[agentpbv2.ROS2HzSample]{ctx: context.Background()}
	if err := svc.MonitorHz(&agentpbv2.MonitorROS2HzRequest{Topic: "/scan"}, stream); err != nil {
		t.Fatalf("MonitorHz: %v", err)
	}
	if len(stream.sent) != 1 {
		t.Fatalf("got %d samples, want 1", len(stream.sent))
	}
	if stream.sent[0].GetHz() != 30.001 || stream.sent[0].GetWindow() != 31 {
		t.Errorf("sample = %+v", stream.sent[0])
	}
}

func TestROS2Service_ListBags(t *testing.T) {
	bagDir := t.TempDir()
	bagPath := filepath.Join(bagDir, "test-bag")
	if err := os.MkdirAll(bagPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bagPath, "data.db3"), bytes.Repeat([]byte("x"), 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := "rosbag2_bagfile_information:\n  duration:\n    nanoseconds: 2500000000\n"
	if err := os.WriteFile(filepath.Join(bagPath, "metadata.yaml"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := newTestROS2Service(t, &fakeROS2Runtime{}, bagDir)
	resp, err := svc.ListBags(context.Background(), &agentpbv2.ListROS2BagsRequest{})
	if err != nil {
		t.Fatalf("ListBags: %v", err)
	}
	if len(resp.GetBags()) != 1 {
		t.Fatalf("got %d bags, want 1", len(resp.GetBags()))
	}
	bag := resp.GetBags()[0]
	if bag.GetName() != "test-bag" || bag.GetSizeBytes() < 1000 || bag.GetDurationSeconds() != 2.5 {
		t.Errorf("bag = %+v", bag)
	}
}

func TestROS2Service_ListBags_MissingDirIsEmpty(t *testing.T) {
	svc := newTestROS2Service(t, &fakeROS2Runtime{}, filepath.Join(t.TempDir(), "nonexistent"))
	resp, err := svc.ListBags(context.Background(), &agentpbv2.ListROS2BagsRequest{})
	if err != nil || len(resp.GetBags()) != 0 {
		t.Errorf("got (%v, %v), want empty response", resp, err)
	}
}

func TestROS2Service_DownloadBag(t *testing.T) {
	bagDir := t.TempDir()
	bagPath := filepath.Join(bagDir, "dl-bag")
	if err := os.MkdirAll(bagPath, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello rosbag")
	if err := os.WriteFile(filepath.Join(bagPath, "metadata.yaml"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := newTestROS2Service(t, &fakeROS2Runtime{}, bagDir)
	stream := &fakeServerStream[agentpbv2.ROS2BagChunk]{ctx: context.Background()}
	if err := svc.DownloadBag(&agentpbv2.DownloadROS2BagRequest{Name: "dl-bag"}, stream); err != nil {
		t.Fatalf("DownloadBag: %v", err)
	}

	var archive bytes.Buffer
	for _, chunk := range stream.sent {
		archive.Write(chunk.GetData())
	}
	tr := tar.NewReader(&archive)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar: %v", err)
		}
		names = append(names, hdr.Name)
		if hdr.Name == "dl-bag/metadata.yaml" {
			data, _ := io.ReadAll(tr)
			if !bytes.Equal(data, content) {
				t.Errorf("metadata content = %q", data)
			}
		}
	}
	if len(names) != 2 { // dir entry + file
		t.Errorf("archive entries = %v, want bag dir + metadata.yaml", names)
	}

	// Path traversal and unknown names must be rejected.
	if err := svc.DownloadBag(&agentpbv2.DownloadROS2BagRequest{Name: "../etc"}, stream); status.Code(err) != codes.InvalidArgument {
		t.Errorf("traversal error = %v, want InvalidArgument", err)
	}
	if err := svc.DownloadBag(&agentpbv2.DownloadROS2BagRequest{Name: "missing"}, stream); status.Code(err) != codes.NotFound {
		t.Errorf("missing bag error = %v, want NotFound", err)
	}
}

func TestROS2Service_Exec(t *testing.T) {
	rt := &fakeROS2Runtime{
		sidecar: ROS2Sidecar{Distro: "humble"},
		execFn: func(ctx context.Context, opts ROS2ExecOptions, stdout, stderr io.Writer) (int, error) {
			io.WriteString(stdout, "out-line\n")
			io.WriteString(stderr, "err-line\n")
			return 3, nil
		},
	}
	svc := newTestROS2Service(t, rt, t.TempDir())
	stream := &fakeServerStream[agentpbv2.ROS2ExecOutput]{ctx: context.Background()}
	if err := svc.Exec(&agentpbv2.ROS2ExecRequest{Args: []string{"topic", "bw", "/scan"}}, stream); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	var stdout, stderr string
	var exitCode *int32
	for _, msg := range stream.sent {
		stdout += string(msg.GetStdout())
		stderr += string(msg.GetStderr())
		if msg.ExitCode != nil {
			exitCode = msg.ExitCode
		}
	}
	if stdout != "out-line\n" || stderr != "err-line\n" {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
	if exitCode == nil || *exitCode != 3 {
		t.Errorf("exit code = %v, want 3", exitCode)
	}

	if err := svc.Exec(&agentpbv2.ROS2ExecRequest{}, stream); status.Code(err) != codes.InvalidArgument {
		t.Errorf("empty args error = %v, want InvalidArgument", err)
	}
}
