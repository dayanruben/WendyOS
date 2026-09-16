package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/a2a"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Name: "test-agent", Profile: "device-reasoning", Workspace: t.TempDir()}
}
func request(id, text string) a2a.SendRequest {
	return a2a.SendRequest{Message: a2a.Message{MessageID: id, Role: "ROLE_USER", Parts: []a2a.Part{{Text: text}}}}
}
func testService(t *testing.T, c Config, runner Runner) *Service {
	t.Helper()
	s, err := Open(c, t.TempDir(), runner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func run(t *testing.T, s *Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("worker did not stop")
		}
	}
}
func TestDurableQueueAndDeduplication(t *testing.T) {
	c := testConfig(t)
	dir := t.TempDir()
	var count atomic.Int32
	runner := func(context.Context, string) (string, error) { count.Add(1); return "verified", nil }
	s, err := Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(request("one", "inspect"))
	if err != nil {
		t.Fatal(err)
	}
	dup, err := s.Submit(request("one", "inspect"))
	if err != nil || dup.ID != task.ID {
		t.Fatal("duplicate task", dup, err)
	}
	if _, err := s.Submit(request("one", "different")); !errors.Is(err, ErrConflict) {
		t.Fatal("changed request was accepted", err)
	}
	if _, err := Open(c, dir, runner); err == nil {
		t.Fatal("second process acquired state lock")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stop := run(t, s)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done, err := s.Wait(ctx, task.ID)
	if err != nil || done.Status.State != a2a.Completed || count.Load() != 1 {
		t.Fatal(done, err, count.Load())
	}
	if done.Artifacts[0].Parts[0].Text != "verified" {
		t.Fatal(done)
	}
}
func TestRestartNeverReplaysWorkingTask(t *testing.T) {
	c := testConfig(t)
	dir := t.TempDir()
	runner := func(context.Context, string) (string, error) { t.Error("replayed interrupted task"); return "", nil }
	s, err := Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := s.Submit(request("one", "change device"))
	s.mu.Lock()
	err = s.change(func(db *database) error { find(db, task.ID).Task.Status.State = a2a.Working; return nil })
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	done, err := s.Get(task.ID)
	if err != nil || done.Status.State != a2a.Failed || !strings.Contains(done.Status.Message.Parts[0].Text, "Inspect") {
		t.Fatal(done, err)
	}
}
func TestEventsFreshnessThresholdCooldownAndRestart(t *testing.T) {
	c := testConfig(t)
	c.Triggers = []Trigger{{Source: "camera", Type: "person", MinConfidence: .8, Consecutive: 2, MaxGapSeconds: 5, MaxAgeSeconds: 30, CooldownSeconds: 10, Prompt: "inspect"}}
	dir := t.TempDir()
	runner := func(context.Context, string) (string, error) { return "ok", nil }
	s, err := Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	confidence := .9
	event := SensorEvent{ID: "1", Type: "person", Source: "camera", Confidence: &confidence, Timestamp: now, Data: json.RawMessage(`{"frame":"one"}`)}
	first, err := s.Event(event)
	if err != nil || first.Accepted || first.Reason != "waiting for consecutive observations" {
		t.Fatal(first, err)
	}
	duplicate, err := s.Event(event)
	if err != nil || duplicate.Accepted {
		t.Fatal(duplicate, err)
	}
	_ = s.Close()
	s, err = Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.now = func() time.Time { return now }
	now = now.Add(time.Second)
	event.ID = "2"
	event.Timestamp = now
	second, err := s.Event(event)
	if err != nil || !second.Accepted || second.TaskID == "" {
		t.Fatal(second, err)
	}
	now = now.Add(time.Second)
	event.ID = "3"
	event.Timestamp = now
	cooldown, err := s.Event(event)
	if err != nil || cooldown.Accepted || cooldown.Reason != "cooldown" {
		t.Fatal(cooldown, err)
	}
	event.ID = "4"
	event.Timestamp = now.Add(-time.Minute)
	stale, err := s.Event(event)
	if err != nil || stale.Accepted || !strings.Contains(stale.Reason, "stale") {
		t.Fatal(stale, err)
	}
	now = now.Add(time.Minute)
	stop := run(t, s)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done, err := s.Wait(ctx, second.TaskID)
	if err != nil || done.Status.State != a2a.Rejected {
		t.Fatal("expired event executed", done, err)
	}
}
func TestCancellationStopsWorkerBeforeNextTask(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	s := testService(t, testConfig(t), func(ctx context.Context, prompt string) (string, error) {
		started <- prompt
		if prompt == "first" {
			<-ctx.Done()
			<-release
			return "", ctx.Err()
		}
		return "done", nil
	})
	first, _ := s.Submit(request("1", "first"))
	second, _ := s.Submit(request("2", "second"))
	stop := run(t, s)
	defer stop()
	<-started
	canceled, err := s.Cancel(first.ID)
	if err != nil || canceled.Status.State != a2a.Canceled {
		t.Fatal(canceled, err)
	}
	select {
	case <-started:
		t.Fatal("next task ran while canceled task still executing")
	default:
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done, err := s.Wait(ctx, second.ID)
	if err != nil || done.Status.State != a2a.Completed {
		t.Fatal(done, err)
	}
}
func TestA2AHTTPAuthenticationAndLifecycle(t *testing.T) {
	s := testService(t, testConfig(t), func(context.Context, string) (string, error) { return "done", nil })
	stop := run(t, s)
	defer stop()
	const token = "test-access-token-123"
	handler, err := s.Handler("http://127.0.0.1:8787", token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := a2a.NewClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Send(context.Background(), request("1", "hello"))
	if err != nil || response.Task.Status.State != a2a.Completed {
		t.Fatal(response, err)
	}
	task, err := client.Get(context.Background(), response.Task.ID)
	if err != nil || task.ID != response.Task.ID {
		t.Fatal(task, err)
	}
	var list a2a.TaskList
	if err := client.Do(context.Background(), "GET", "/tasks", nil, &list); err != nil || len(list.Tasks) != 1 {
		t.Fatal(list, err)
	}
	wrong, _ := a2a.NewClient(server.URL, "wrong")
	if _, err := wrong.Get(context.Background(), task.ID); err == nil {
		t.Fatal("unauthenticated task access")
	}
	resp, err := http.Get(server.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var card map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&card)
	if card["name"] != "test-agent" || card["supportedInterfaces"] == nil || card["securityRequirements"] == nil {
		t.Fatal(card)
	}
	req, _ := http.NewRequest("GET", server.URL+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("A2A-Version", "0.3")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatal("unsupported version accepted")
	}
}
func TestConfigChangeAndBackpressure(t *testing.T) {
	c := testConfig(t)
	c.MaxTasks = 1
	dir := t.TempDir()
	runner := func(context.Context, string) (string, error) { return "", nil }
	s, err := Open(c, dir, runner)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Submit(request("1", "one"))
	if _, err := s.Submit(request("2", "two")); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	_ = s.Close()
	c.Profile = "debugger"
	if changed, err := Open(c, dir, runner); err == nil {
		changed.Close()
		t.Fatal("configuration changed under pending tasks")
	}
}

func TestStorageFailureStopsFurtherExecution(t *testing.T) {
	s := testService(t, testConfig(t), func(context.Context, string) (string, error) {
		t.Error("ran after persistence failure")
		return "", nil
	})
	// Renaming a state file over an existing directory must fail on every OS.
	s.file = t.TempDir()
	if _, err := s.Submit(request("1", "inspect")); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	if len(s.Tasks()) != 0 {
		t.Fatal("published an uncommitted task")
	}
	if err := s.Run(context.Background()); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
	if _, err := s.Submit(request("2", "retry")); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
}

func TestRemoteDelegationDepthSurvivesQueue(t *testing.T) {
	s := testService(t, testConfig(t), func(ctx context.Context, _ string) (string, error) {
		if a2a.DelegationDepth(ctx) != 3 {
			t.Error("lost remote delegation depth")
		}
		return "ok", nil
	})
	req := request("depth", "inspect")
	req.Metadata = map[string]any{a2a.DelegationDepthKey: 3}
	task, err := s.Submit(req)
	if err != nil {
		t.Fatal(err)
	}
	stop := run(t, s)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := s.Wait(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	req.Message.MessageID = "too-deep"
	req.Metadata[a2a.DelegationDepthKey] = 5
	if _, err := s.Submit(req); err == nil {
		t.Fatal("accepted unbounded delegation")
	}
}
