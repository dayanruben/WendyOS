// Package oshealth implements post-OS-update healthchecks for critical
// system services and the commit-or-rollback decision for pending Mender
// A/B updates.
package oshealth

import (
	"time"
)

const (
	// DefaultStateDir is on the WendyOS data partition, which is shared
	// between both A/B rootfs slots — state written here before a reboot is
	// visible to whichever slot boots next.
	DefaultStateDir = "/data/wendy-agent"

	pendingMarkerFile = "pending-update.json"

	// MaxPendingMarkerAge bounds how long a pending-update marker is trusted.
	// A marker can outlive its update when the freshly booted slot ships an
	// agent without healthcheck support, which commits without consuming it.
	MaxPendingMarkerAge = time.Hour
)

// PendingMarker records that an OS update was installed and a reboot into the
// new slot is pending verification. It is written right before the
// post-install reboot.
type PendingMarker struct {
	CreatedAt    time.Time `json:"created_at"`
	OldOSVersion string    `json:"old_os_version,omitempty"`
	ArtifactURL  string    `json:"artifact_url,omitempty"`
	AgentVersion string    `json:"agent_version,omitempty"`
}

func WritePendingMarker(dir string, m PendingMarker) error {
	return writeJSONAtomic(dir, pendingMarkerFile, m)
}

func ReadPendingMarker(dir string) (PendingMarker, bool, error) {
	var m PendingMarker
	found, err := readJSON(dir, pendingMarkerFile, &m)
	if err != nil {
		return PendingMarker{}, false, err
	}
	return m, found, nil
}

func ClearPendingMarker(dir string) error {
	return removeIfExists(dir, pendingMarkerFile)
}
