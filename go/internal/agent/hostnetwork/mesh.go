// Package hostnetwork manages host-level network primitives that live
// outside any single container's network namespace, such as the iptables
// chain used by the wendy-mesh CNI plugin.
package hostnetwork

import (
	"fmt"
	"os/exec"
	"strings"
)

// MeshChainName is the name of the iptables filter-table chain that holds
// per-container ACCEPT rules for mesh-enabled containers. The wendy-mesh CNI
// plugin (a separate repo/binary) appends rules into this chain when a
// container with the "mesh" network entitlement is created.
const MeshChainName = "WENDY-MESH"

// InitMeshChain ensures the WENDY-MESH chain exists in the filter table and
// that FORWARD jumps into it. It is idempotent and safe to call on every
// agent startup:
//   - creating the chain is a no-op if the chain already exists (and any
//     ACCEPT rules the CNI plugin already added into it are left untouched —
//     this never clears the chain),
//   - the FORWARD jump rule is only appended if it isn't already present, so
//     repeated calls never produce duplicate jump rules.
//
// The chain starts out empty, so until the CNI plugin adds per-container
// ACCEPT rules, the jump is a no-op and existing FORWARD behaviour (whatever
// it falls through to) is unaffected.
//
// This shells out to the system iptables binary rather than depending on a
// Go iptables library, matching how the agent already shells out to other
// host tools (e.g. nmcli in internal/agent/network). It is safe to call on
// hosts without iptables (e.g. non-Linux dev machines) or without sufficient
// privileges — callers should treat a returned error as non-fatal.
func InitMeshChain() error {
	if err := ensureChain(MeshChainName); err != nil {
		return fmt.Errorf("hostnetwork: ensure chain %s: %w", MeshChainName, err)
	}
	if err := ensureForwardJump(MeshChainName); err != nil {
		return fmt.Errorf("hostnetwork: ensure FORWARD jump to %s: %w", MeshChainName, err)
	}
	return nil
}

// TeardownMeshChain removes the FORWARD jump rule and deletes the WENDY-MESH
// chain. It is idempotent: attempting to remove a rule or chain that is
// already absent is treated as success. Any per-container ACCEPT rules the
// CNI plugin has not yet cleaned up will cause the chain deletion to fail
// (iptables refuses to delete a non-empty chain that is still referenced),
// which is surfaced to the caller as an error rather than silently flushed.
func TeardownMeshChain() error {
	if err := deleteForwardJumpIfPresent(MeshChainName); err != nil {
		return fmt.Errorf("hostnetwork: remove FORWARD jump to %s: %w", MeshChainName, err)
	}
	if err := deleteChainIfPresent(MeshChainName); err != nil {
		return fmt.Errorf("hostnetwork: delete chain %s: %w", MeshChainName, err)
	}
	return nil
}

// ensureChain creates the named filter-table chain if it does not already
// exist. `iptables -N <chain>` exits non-zero with "Chain already exists" on
// stderr when the chain is present, which is treated as success.
func ensureChain(chain string) error {
	out, err := exec.Command("iptables", "-t", "filter", "-N", chain).CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(out), "already exists") {
		return nil
	}
	return fmt.Errorf("iptables -N %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
}

// ensureForwardJump appends a `FORWARD -j <chain>` rule only if one is not
// already present, so repeated calls never create duplicate jump rules.
func ensureForwardJump(chain string) error {
	exists, err := forwardJumpExists(chain)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	out, err := exec.Command("iptables", "-t", "filter", "-A", "FORWARD", "-j", chain).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -A FORWARD -j %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// forwardJumpExists checks for an existing `FORWARD -j <chain>` rule via
// `iptables -C`, whose exit code directly encodes presence: 0 means the rule
// exists, 1 means it doesn't, and anything else is a real error (e.g.
// missing permissions or a malformed rule specification).
func forwardJumpExists(chain string) (bool, error) {
	cmd := exec.Command("iptables", "-t", "filter", "-C", "FORWARD", "-j", chain)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("iptables -C FORWARD -j %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
}

// deleteForwardJumpIfPresent removes the FORWARD jump rule if it exists,
// treating an already-absent rule as success.
func deleteForwardJumpIfPresent(chain string) error {
	exists, err := forwardJumpExists(chain)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	out, err := exec.Command("iptables", "-t", "filter", "-D", "FORWARD", "-j", chain).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -D FORWARD -j %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// deleteChainIfPresent flushes and deletes the named chain if it exists,
// treating an already-absent chain as success.
func deleteChainIfPresent(chain string) error {
	if err := exec.Command("iptables", "-t", "filter", "-S", chain).Run(); err != nil {
		// Chain doesn't exist (or isn't inspectable); nothing to tear down.
		return nil
	}
	if out, err := exec.Command("iptables", "-t", "filter", "-F", chain).CombinedOutput(); err != nil {
		return fmt.Errorf("iptables -F %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("iptables", "-t", "filter", "-X", chain).CombinedOutput(); err != nil {
		return fmt.Errorf("iptables -X %s: %w (%s)", chain, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// exitCode extracts the process exit code from an error returned by
// exec.Cmd.Run/CombinedOutput, returning -1 if it isn't an *exec.ExitError
// (e.g. the binary could not be found or started at all).
func exitCode(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
