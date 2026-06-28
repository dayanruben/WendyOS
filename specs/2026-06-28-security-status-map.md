
---

## Framing & disposition decisions (controller, 2026-06-28)

These override how the items are presented in the refreshed threat model and the public pages.
They are accepted design tradeoffs with documented mitigations — NOT "open CRITICAL/HIGH gaps":

- **WDY-1006 / TM-S-01 (plaintext pre-provisioning):** Accepted, expected behavior for an
  *unprovisioned* device. The plaintext state is clearly warned, time-bounded, and provisioning
  is offered during tools installation — provisioning IS the mitigation. Present it as an
  intentional, documented state (mitigations-first: "unprovisioned devices use a plaintext
  channel by design; provision to establish mTLS"), NOT as a CRITICAL unauthenticated-RCE flaw.
  Do not enumerate exposed RPCs alarmingly or publish a CRITICAL severity badge for it publicly.
- **WDY-1008 / camera /dev bind:** Mitigated by design. The full `/dev` bind is intentional
  (USB webcam hotplug); device access is gated by per-major cgroup deny-all-baseline allow rules.
  Present as a deliberate, mitigated design choice, not a gap.

All other ✅/🛠/📋 statuses in this map stand.
