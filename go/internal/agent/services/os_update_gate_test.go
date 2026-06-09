package services

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/agent/oshealth"
)

func TestRecordPendingOSUpdateRedactsURLCredentials(t *testing.T) {
	dir := t.TempDir()

	recordPendingOSUpdate(zap.NewNop(), dir, "https://user:hunter2@artifacts.example.com/os.mender")

	marker, found, err := oshealth.ReadPendingMarker(dir)
	if err != nil || !found {
		t.Fatalf("marker should be written (found=%v err=%v)", found, err)
	}
	if strings.Contains(marker.ArtifactURL, "hunter2") {
		t.Errorf("persisted ArtifactURL %q must not contain credentials", marker.ArtifactURL)
	}
	if !strings.Contains(marker.ArtifactURL, "artifacts.example.com/os.mender") {
		t.Errorf("persisted ArtifactURL %q should keep host and path for debugging", marker.ArtifactURL)
	}
}

func TestRecordPendingOSUpdateClearsPreviousResult(t *testing.T) {
	dir := t.TempDir()
	prev := oshealth.UpdateResult{
		Outcome:   oshealth.OutcomeCommitted,
		CreatedAt: time.Now().Add(-10 * time.Minute),
	}
	if err := oshealth.WriteUpdateResult(dir, prev); err != nil {
		t.Fatal(err)
	}

	recordPendingOSUpdate(zap.NewNop(), dir, "http://example/artifact.mender")

	if _, found, err := oshealth.ReadUpdateResult(dir); err != nil || found {
		t.Errorf("previous update result must be cleared so it cannot be mistaken for this attempt's outcome (found=%v err=%v)", found, err)
	}
	marker, found, err := oshealth.ReadPendingMarker(dir)
	if err != nil || !found {
		t.Fatalf("marker should be written (found=%v err=%v)", found, err)
	}
	if marker.ArtifactURL != "http://example/artifact.mender" {
		t.Errorf("ArtifactURL = %q", marker.ArtifactURL)
	}
}
