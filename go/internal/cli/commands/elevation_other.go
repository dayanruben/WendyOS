//go:build !windows

package commands

// elevateForT234Recovery is Windows-only: UAC elevation relaunches the whole
// process, so it must happen before the interactive wizard collects answers.
// macOS/Linux privilege handling stays where it was — sudo pre-auth inside
// installOrin — because sudo elevates in place without losing any state.
func elevateForT234Recovery(string) error { return nil }

// processElevated is Windows-only; elsewhere there is no UAC handoff whose
// guidance would need suppressing.
func processElevated() bool { return false }
