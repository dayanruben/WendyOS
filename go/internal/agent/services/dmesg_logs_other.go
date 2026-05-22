//go:build !linux

package services

import "context"

// CollectDmesgLogs is a no-op on non-Linux platforms.
func CollectDmesgLogs(_ context.Context, _ *TelemetryBroadcaster) {}
