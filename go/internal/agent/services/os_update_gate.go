package services

import (
	"context"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/agent/oshealth"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

// RunOSUpdateGate confirms or rolls back a pending A/B update at agent startup.
// When an OS update is pending verification (marker written by UpdateOS before
// the reboot), it healthchecks the critical services first and rolls back to
// the previous OS if any of them failed to come up. Without a pending update it
// reduces to the plain "commit" the agent has always run on startup.
//
// The commit/rollback are driven by the backend that installed the update
// (recorded in the marker): the in-house wendyos-update engine, or mender as a
// fallback. The healthcheck logic itself is backend-agnostic.
//
// This must run before the gRPC server starts: the device must not appear back
// online to a waiting `wendy os update` until the commit-or-rollback decision
// is made.
func RunOSUpdateGate(logger *zap.Logger) {
	marker, found, err := oshealth.ReadPendingMarker(oshealth.DefaultStateDir)
	if err != nil {
		// The gate re-reads and discards a corrupt marker itself; here we only
		// need a backend to drive the commit, so fall through to auto-select.
		logger.Warn("Pending OS update marker is unreadable; auto-selecting the update backend", zap.Error(err))
	}
	requested := requestedBackendFromMarker(marker, found && err == nil)
	commit, rollback := commitClosures(logger, requested)

	gate := &oshealth.Gate{
		Logger:   logger,
		StateDir: oshealth.DefaultStateDir,
		Services: oshealth.DefaultCriticalServices,
		Checker:  oshealth.NewChecker(logger),
		Commit:   commit,
		Rollback: rollback,
		Reboot:   rebootSystem,
		OSVersion: func() string {
			v, _ := wendyOSVersion()
			return v
		},
	}
	gate.Run(context.Background())
}

// requestedBackendFromMarker returns the backend the gate should drive: the one
// the marker recorded, or "" (auto-select) when there is no marker or it
// predates multi-backend support.
func requestedBackendFromMarker(marker oshealth.PendingMarker, found bool) string {
	if found {
		return marker.Backend
	}
	return ""
}

// commitClosures resolves the backend for the gate and returns its
// commit/rollback. It selects by binary presence (chooseUpdaterForCommit), not
// the install-time connector probe: the update has already been installed, so a
// transient probe failure must not block committing a healthy slot. If no
// backend binary is present (e.g. a non-WendyOS host), it returns no-ops
// reporting "unavailable" so the gate degrades to the pre-multi-backend no-op.
func commitClosures(logger *zap.Logger, requested string) (commit, rollback func() oshealth.MenderResult) {
	updater := chooseUpdaterForCommit(requested, productionUpdaters(logger))
	if updater == nil {
		logger.Debug("No OS update backend available for the gate; commit/rollback are no-ops",
			zap.String("requested", requested))
		noop := func() oshealth.MenderResult { return oshealth.MenderResult{Status: oshealth.MenderUnavailable} }
		return noop, noop
	}
	return updater.commit, updater.rollback
}

// recordPendingOSUpdate persists the pending-update marker that the healthcheck
// gate consumes on the next boot, tagging it with the backend that installed
// the update. Failure is fail-open: without a marker the next boot performs a
// plain commit, matching the pre-healthcheck behavior.
func recordPendingOSUpdate(logger *zap.Logger, stateDir, artifactURL, backend string) {
	// A result record left over from a previous attempt must not be mistaken
	// for this update's outcome, so drop it before the reboot.
	if err := oshealth.ClearUpdateResult(stateDir); err != nil {
		logger.Warn("Failed to clear previous OS update result record", zap.Error(err))
	}
	marker := oshealth.PendingMarker{
		CreatedAt:    time.Now(),
		ArtifactURL:  redactURLCredentials(artifactURL),
		AgentVersion: version.Version,
		BootID:       oshealth.CurrentBootID(),
		Backend:      backend,
	}
	if v, ok := wendyOSVersion(); ok {
		marker.OldOSVersion = v
	}
	if err := oshealth.WritePendingMarker(stateDir, marker); err != nil {
		logger.Warn("Failed to write pending OS update marker; post-reboot healthchecks will be skipped",
			zap.Error(err))
	}
}

// redactURLCredentials masks any credentials in a URL before it is persisted or
// logged; the marker only needs the URL for debugging. It strips the userinfo
// password and redacts every query-string value, since presigned/OTA artifact
// URLs carry their auth material in the query (e.g. X-Amz-Signature, token),
// which url.Redacted alone leaves in cleartext. It fails closed: a URL that
// cannot be parsed is dropped rather than echoed, since it may embed
// credentials we cannot locate.
func redactURLCredentials(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "<redacted: unparseable URL>"
	}
	if values := u.Query(); len(values) > 0 {
		for key := range values {
			values[key] = []string{"REDACTED"}
		}
		u.RawQuery = values.Encode()
	}
	return u.Redacted()
}
