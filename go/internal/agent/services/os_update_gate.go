package services

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/wendylabsinc/wendy/go/internal/agent/oshealth"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

// RunOSUpdateGate confirms or rolls back a pending Mender A/B update at agent
// startup. When an OS update is pending verification (marker written by
// UpdateOS before the reboot), it healthchecks the critical services first
// and rolls back to the previous OS if any of them failed to come up. Without
// a pending update it reduces to the plain "mender-update commit" the agent
// has always run on startup.
//
// This must run before the gRPC server starts: the device must not appear
// back online to a waiting `wendy os update` until the commit-or-rollback
// decision is made.
func RunOSUpdateGate(logger *zap.Logger) {
	gate := &oshealth.Gate{
		Logger:   logger,
		StateDir: oshealth.DefaultStateDir,
		Services: oshealth.DefaultCriticalServices,
		Checker:  oshealth.NewChecker(logger),
		Commit:   func() oshealth.MenderResult { return menderRun(logger, "commit") },
		Rollback: func() oshealth.MenderResult { return menderRun(logger, "rollback") },
		Reboot:   rebootSystem,
		OSVersion: func() string {
			v, _ := wendyOSVersion()
			return v
		},
	}
	gate.Run(context.Background())
}

// menderCommandTimeout bounds a single mender-update commit/rollback. These are
// fast bootloader-metadata operations; the timeout exists only so a hung mender
// (the likely failure mode exactly when systemd/D-Bus/storage is unhealthy
// early in boot) cannot block agent startup — and therefore the
// commit-or-rollback decision — indefinitely.
const menderCommandTimeout = 60 * time.Second

// menderRun executes "mender-update <subcommand>" and classifies the result.
// Exit code 2 means "nothing pending" for both commit and rollback. If the
// update is never committed, Mender rolls back on the next reboot.
func menderRun(logger *zap.Logger, subcommand string) oshealth.MenderResult {
	binary, found := resolveMenderBinary()
	if !found {
		return oshealth.MenderResult{Status: oshealth.MenderUnavailable}
	}
	ctx, cancel := context.WithTimeout(context.Background(), menderCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, subcommand)
	cmd.Env = envWithPath("/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logger.Error("mender-update timed out",
				zap.String("subcommand", subcommand),
				zap.Duration("timeout", menderCommandTimeout),
				zap.String("output", output))
			return oshealth.MenderResult{Status: oshealth.MenderError, Output: output, Err: ctx.Err()}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return oshealth.MenderResult{Status: oshealth.MenderNothingPending, Output: output}
		}
		logger.Warn("mender-update invocation failed",
			zap.String("subcommand", subcommand), zap.String("output", output), zap.Error(err))
		return oshealth.MenderResult{Status: oshealth.MenderError, Output: output, Err: err}
	}
	return oshealth.MenderResult{Status: oshealth.MenderOK, Output: output}
}

// recordPendingOSUpdate persists the pending-update marker that the
// healthcheck gate consumes on the next boot. Failure is fail-open: without a
// marker the next boot performs a plain commit, matching the pre-healthcheck
// behavior.
func recordPendingOSUpdate(logger *zap.Logger, stateDir, artifactURL string) {
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
