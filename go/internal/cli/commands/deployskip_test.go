package commands

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/proto/gen/agentpb"
	"google.golang.org/grpc"
)

func TestServiceFingerprintKey(t *testing.T) {
	a := serviceFingerprintKey("sh.wendy.app", "gpu")
	b := serviceFingerprintKey("sh.wendy.app", "vui")
	if a == b {
		t.Fatalf("distinct services share a fingerprint key: %q", a)
	}
	if want := "sh.wendy.app/svc/gpu"; a != want {
		t.Fatalf("serviceFingerprintKey = %q, want %q", a, want)
	}
}

// With WENDY_PUSH_SKIP=0 the planner must short-circuit before touching the
// device (so a nil conn is safe) and skip nothing.
func TestPlanServicePushSkipsDisabled(t *testing.T) {
	t.Setenv("WENDY_PUSH_SKIP", "0")
	services := map[string]*appconfig.ServiceConfig{
		"a": {Context: "./a"},
		"b": {Context: "./b"},
	}
	skip, hashes := planServicePushSkips(context.Background(), nil, t.TempDir(), "app", "devkey", "linux/arm64", services, nil)
	if len(skip) != 0 {
		t.Fatalf("expected no skips when disabled, got %v", skip)
	}
	if len(hashes) != 0 {
		t.Fatalf("expected no hashes computed when disabled, got %v", hashes)
	}
}

// multiSvcContainerClient reports one app group with a set of service names
// present, and answers QueryLayers from a configurable present-layer set. It
// drives planServicePushSkips end to end.
type multiSvcContainerClient struct {
	agentpb.WendyContainerServiceClient // embedded nil

	appName       string
	services      []string
	presentLayers map[string]bool
}

func (f *multiSvcContainerClient) ListContainers(_ context.Context, _ *agentpb.ListContainersRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentpb.ListContainersResponse], error) {
	svcs := make([]*agentpb.ServiceEntry, 0, len(f.services))
	for _, s := range f.services {
		svcs = append(svcs, &agentpb.ServiceEntry{Name: s})
	}
	return &fakeListContainersStream{resp: &agentpb.ListContainersResponse{
		Container: &agentpb.AppContainer{AppName: f.appName, Services: svcs},
	}}, nil
}

func (f *multiSvcContainerClient) QueryLayers(_ context.Context, in *agentpb.QueryLayersRequest, _ ...grpc.CallOption) (*agentpb.QueryLayersResponse, error) {
	resp := &agentpb.QueryLayersResponse{}
	for _, id := range in.GetDiffIds() {
		if f.presentLayers[id] {
			resp.Present = append(resp.Present, &agentpb.PresentLayer{DiffId: id, Size: 1})
		}
	}
	return resp, nil
}

// writeServiceContext lays down a minimal buildable context (Dockerfile) under
// cwd/<rel> so computeBuildInputHash succeeds, and returns the hash the planner
// will compute for it.
func writeServiceContext(t *testing.T, cwd, rel, platform string) string {
	t.Helper()
	writeFile(t, filepath.Join(cwd, rel), "Dockerfile", "FROM scratch\n")
	h, err := computeBuildInputHash(filepath.Join(cwd, rel), "", platform, nil)
	if err != nil {
		t.Fatalf("computeBuildInputHash: %v", err)
	}
	return h
}

// TestPlanServicePushSkips_MissingLayersForcesRebuild is the multi-service side
// of WDY-1824: a service whose inputs are unchanged AND whose container is
// present must still NOT be skipped when the device no longer holds its recorded
// image layers.
func TestPlanServicePushSkips_MissingLayersForcesRebuild(t *testing.T) {
	isolateFingerprintCache(t)

	const (
		appID     = "grp"
		deviceKey = "devkey"
		platform  = "linux/arm64"
	)
	cwd := t.TempDir()
	hash := writeServiceContext(t, cwd, "llm", platform)

	// Fingerprint recorded a layer; matching inputs; container is present.
	saveDeployFingerprint(serviceFingerprintKey(appID, "llm"), deviceKey, deployFingerprint{InputHash: hash, LayerDiffIDs: []string{"sha256:layer0"}})

	services := map[string]*appconfig.ServiceConfig{"llm": {Context: "./llm"}}

	// Device: container present, but layer NOT present (stale/partial image).
	fake := &multiSvcContainerClient{appName: appID, services: []string{"llm"}, presentLayers: map[string]bool{}}
	conn := &grpcclient.AgentConnection{ContainerService: fake}

	skip, hashes := planServicePushSkips(context.Background(), conn, cwd, appID, deviceKey, platform, services, nil)
	if skip["llm"] {
		t.Fatal("service skipped despite the device missing its image layers (WDY-1824)")
	}
	if hashes["llm"] != hash {
		t.Fatalf("hash for llm = %q, want %q", hashes["llm"], hash)
	}
}

// TestPlanServicePushSkips_ContentPresentSkips confirms the optimization still
// fires when everything lines up: unchanged inputs, present container, and every
// recorded layer confirmed present on the device.
func TestPlanServicePushSkips_ContentPresentSkips(t *testing.T) {
	isolateFingerprintCache(t)

	const (
		appID     = "grp"
		deviceKey = "devkey"
		platform  = "linux/arm64"
		layerID   = "sha256:layer0"
	)
	cwd := t.TempDir()
	hash := writeServiceContext(t, cwd, "llm", platform)
	saveDeployFingerprint(serviceFingerprintKey(appID, "llm"), deviceKey, deployFingerprint{InputHash: hash, LayerDiffIDs: []string{layerID}})

	services := map[string]*appconfig.ServiceConfig{"llm": {Context: "./llm"}}
	fake := &multiSvcContainerClient{appName: appID, services: []string{"llm"}, presentLayers: map[string]bool{layerID: true}}
	conn := &grpcclient.AgentConnection{ContainerService: fake}

	skip, _ := planServicePushSkips(context.Background(), conn, cwd, appID, deviceKey, platform, services, nil)
	if !skip["llm"] {
		t.Fatal("expected llm to be skipped: inputs unchanged, container present, layers present")
	}
}
