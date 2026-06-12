package commands

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

func testROS2Graph() *agentpbv2.GetROS2GraphResponse {
	return &agentpbv2.GetROS2GraphResponse{
		Nodes: []*agentpbv2.ROS2Node{
			{Name: "lidar_driver", Namespace: "/"},
			{Name: "slam_node", Namespace: "/"},
			{Name: "idle_node", Namespace: "/"},
		},
		Publishes: []*agentpbv2.GetROS2GraphResponse_Edge{
			{Node: "/lidar_driver", Topic: "/scan"},
			{Node: "/lidar_driver", Topic: "/rosout"},
			{Node: "/slam_node", Topic: "/map"},
		},
		Subscribes: []*agentpbv2.GetROS2GraphResponse_Edge{
			{Node: "/slam_node", Topic: "/scan"},
			{Node: "/slam_node", Topic: "/parameter_events"},
		},
	}
}

func TestRenderROS2GraphASCII(t *testing.T) {
	out := renderROS2GraphASCII(testROS2Graph())
	if !strings.Contains(out, "[/lidar_driver] ──/scan──▶ [/slam_node]") {
		t.Errorf("missing scan edge in:\n%s", out)
	}
	if !strings.Contains(out, "[/slam_node] ──/map──▶ (no subscribers)") {
		t.Errorf("missing dangling map edge in:\n%s", out)
	}
	if strings.Contains(out, "/rosout") || strings.Contains(out, "/parameter_events") {
		t.Errorf("infrastructure topics must be hidden:\n%s", out)
	}
	if !strings.Contains(out, "[/idle_node]") || !strings.Contains(out, "Isolated nodes") {
		t.Errorf("isolated node missing:\n%s", out)
	}
}

func TestRenderROS2GraphASCII_Empty(t *testing.T) {
	out := renderROS2GraphASCII(&agentpbv2.GetROS2GraphResponse{})
	if !strings.Contains(out, "No ROS 2 nodes") {
		t.Errorf("empty graph output = %q", out)
	}
}

func TestRenderROS2GraphDOT(t *testing.T) {
	out := renderROS2GraphDOT(testROS2Graph())
	if !strings.HasPrefix(out, "digraph ros2 {") || !strings.HasSuffix(strings.TrimSpace(out), "}") {
		t.Errorf("not valid DOT shape:\n%s", out)
	}
	if !strings.Contains(out, `"/lidar_driver" -> "/slam_node" [label="/scan"];`) {
		t.Errorf("missing edge in:\n%s", out)
	}
	if !strings.Contains(out, `"/idle_node";`) {
		t.Errorf("missing node declaration in:\n%s", out)
	}
}

func TestExtractROS2BagArchive(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "mybag", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	content := []byte("yaml: data")
	if err := tw.WriteHeader(&tar.Header{Name: "mybag/metadata.yaml", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := extractROS2BagArchive(&buf, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "mybag", "metadata.yaml"))
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Errorf("content = %q", data)
	}
}

func TestExtractROS2BagArchive_RejectsTraversal(t *testing.T) {
	for _, evil := range []string{"../escape.txt", "/abs/path.txt"} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		if err := tw.WriteHeader(&tar.Header{Name: evil, Typeflag: tar.TypeReg, Mode: 0o644, Size: 0}); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := extractROS2BagArchive(&buf, t.TempDir()); err == nil {
			t.Errorf("expected error for archive entry %q", evil)
		}
	}
}

func TestROS2DomainPtr(t *testing.T) {
	if got := ros2DomainPtr(-1); got != nil {
		t.Errorf("ros2DomainPtr(-1) = %v, want nil", got)
	}
	if got := ros2DomainPtr(0); got == nil || *got != 0 {
		t.Errorf("ros2DomainPtr(0) = %v, want 0 (domain 0 is valid)", got)
	}
	if got := ros2DomainPtr(42); got == nil || *got != 42 {
		t.Errorf("ros2DomainPtr(42) = %v", got)
	}
}
