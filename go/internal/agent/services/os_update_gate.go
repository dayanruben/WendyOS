package services

import (
	"context"
	"errors"
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

// menderRun executes "mender-update <subcommand>" and classifies the result.
// Exit code 2 means "nothing pending" for both commit and rollback. If the
// update is never committed, Mender rolls back on the next reboot.
func menderRun(logger *zap.Logger, subcommand string) oshealth.MenderResult {
	binary, found := resolveMenderBinary()
	if !found {
		return oshealth.MenderResult{Status: oshealth.MenderUnavailable}
	}
	cmd := exec.Command(binary, subcommand)
	cmd.Env = envWithPath("/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
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
	marker := oshealth.PendingMarker{
		CreatedAt:    time.Now(),
		ArtifactURL:  artifactURL,
		AgentVersion: version.Version,
	}
	if v, ok := wendyOSVersion(); ok {
		marker.OldOSVersion = v
	}
	if err := oshealth.WritePendingMarker(stateDir, marker); err != nil {
		logger.Warn("Failed to write pending OS update marker; post-reboot healthchecks will be skipped",
			zap.Error(err))
	}
}
