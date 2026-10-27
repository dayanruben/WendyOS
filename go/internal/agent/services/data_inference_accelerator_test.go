package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/agent/data"
	"github.com/wendylabsinc/wendy/go/internal/agent/inference"
	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

type warnings struct {
	mu       sync.Mutex
	messages []string
}

func (w *warnings) add(message string) {
	w.mu.Lock()
	w.messages = append(w.messages, message)
	w.mu.Unlock()
}
func (w *warnings) matching(text string) (n int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, message := range w.messages {
		if strings.Contains(message, text) {
			n++
		}
	}
	return n
}

func inspectInference(t *testing.T, service *DataService) *data.InferenceStatus {
	t.Helper()
	inspection, err := service.CampaignInspect(context.Background(), &agentpbv2.DataCampaignInspectRequest{Name: "people-all-cameras"})
	if err != nil {
		t.Fatal(err)
	}
	var plan data.Campaign
	if err := json.Unmarshal(inspection.PlanJson, &plan); err != nil {
		t.Fatal(err)
	}
	return plan.InferenceStatus
}

func TestAgentInferenceReportsTheEngineAndWarnsOnFallback(t *testing.T) {
	service, factory, _, _ := newInferenceTestService(t, true)
	logged := &warnings{}
	service.manager.SetWarnLogger(logged.add)
	session := receiveInference(t, factory.sessions)
	building := &data.AcceleratorStatus{Requested: "auto", Active: "cpu", Candidates: []string{"tensorrt", "cpu"},
		Reason: "the TensorRT engine is not ready yet", Engine: &data.EngineStatus{State: "building", StartedAt: time.Now().Add(-90 * time.Second).UTC().Format(time.RFC3339)}}
	session.results <- inference.Result{Type: "accelerator", Accelerator: building, Stats: &data.InferenceStats{FPS: 2, InferMsP50: 40}}
	deadline := time.Now().Add(8 * time.Second)
	var status *data.InferenceStatus
	for time.Now().Before(deadline) {
		if status = inspectInference(t, service); status.Accelerator != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.Accelerator == nil || status.Accelerator.Active != "cpu" || status.Stats == nil || status.Stats.InferMsP50 != 40 {
		t.Fatalf("status lacks the engine: %+v", status)
	}
	// The build's age is current at inspect, not as old as the last report.
	if elapsed := status.Accelerator.Engine.ElapsedSeconds; elapsed < 89 || elapsed > 120 {
		t.Fatalf("elapsed %v", elapsed)
	}
	if logged.matching("not tensorrt") != 0 {
		t.Fatal("building on the cpu is not a fallback")
	}

	degraded := &data.AcceleratorStatus{Requested: "auto", Active: "cpu", Degraded: true, Reason: "tensorrt disagrees with the cpu: a class score differs by 0.310 (> 0.05)"}
	for range 3 {
		session.results <- inference.Result{Type: "accelerator", Accelerator: degraded}
	}
	for time.Now().Before(deadline) && logged.matching("not tensorrt") == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := logged.matching(`campaign "people-all-cameras" inference runs on cpu, not tensorrt: tensorrt disagrees with the cpu`); n != 1 {
		t.Fatalf("fallback logged %d times: %v", n, logged.messages)
	}
	if status := inspectInference(t, service); !status.Accelerator.Degraded || status.Accelerator.Engine != nil {
		t.Fatalf("degraded status %+v", status.Accelerator)
	}
}

func TestPredictionRecordsNameTheEngine(t *testing.T) {
	job := &campaignInferenceJob{status: data.InferenceStatus{Accelerator: &data.AcceleratorStatus{ModelSHA256: strings.Repeat("7", 64)}}}
	attributes := map[string]any{}
	job.predictionProvenance(attributes, inference.Result{Engine: "tensorrt", Precision: "fp16", InferMs: 5.5})
	if attributes["accelerator"] != "tensorrt" || attributes["precision"] != "fp16" || attributes["infer_ms"] != 5.5 || attributes["model_sha256"] != strings.Repeat("7", 64) {
		t.Fatalf("attributes %v", attributes)
	}
	// An older worker reports none of it, and the record says nothing false.
	attributes = map[string]any{}
	(&campaignInferenceJob{}).predictionProvenance(attributes, inference.Result{})
	if len(attributes) != 0 {
		t.Fatalf("attributes %v", attributes)
	}
}

func TestInferenceHardwareIsDeviceInfo(t *testing.T) {
	hardware := InferenceHardware()
	info := detectGPUInfo()
	if hardware.GPUVendor != info.vendor || hardware.CUDAVersion != info.cudaVersion || hardware.GPUArch != info.gpuArch {
		t.Fatalf("inference hardware %+v differs from device info %+v", hardware, info)
	}
}
