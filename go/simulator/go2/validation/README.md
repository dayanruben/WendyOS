# Go2 validation evidence

These reports distinguish functional compatibility, local producer timing and
actual deployed VM acceptance. All tests use virtual robots.

| Report | Scope |
| --- | --- |
| `native-sdk.json` | Actual pinned Unitree SDK and generated/stock ROS participants in isolated ARM64 Linux containers; commands, postures, LowState CRC/motor ordering and LowCmd watchdog |
| `native-sdk-vm.json` | All 13 finite native checks passed in the managed 4-vCPU/4-GiB VM: signed SDK motion, physical postures, request semantics, CRC/motor ordering and exclusive LowCmd stand/watchdog; source digest recorded |
| `navigation.json`, `navigation-source.json` | Separate ROS goal controller with camera/cloud subscriptions, obstacle and stale-scan scenarios; exact source and visual hashes recorded |
| `navigation-vm.json` | Separate deployed ROS navigation app passed goal arrival, camera/cloud observations, obstacle stop/resume and stale-scan stop without automatic goal resumption in 37.23 seconds; a bounded reactive controller, not Nav2 or a precision navigation benchmark |
| `producer-timing.json`, `producer-timing-source.json` | 30-second local producer benchmark after warm-up; both render views, native state and lidar enabled; no external subscriber load |
| `legacy-isolation.json` | Isolated Linux container packet tests for both legacy and nft backends: IPv4/IPv6 loopback traffic, non-loopback RTPS blocking, ordinary networking, rule ownership/idempotency and dropped network administration capability |
| `vm-full-load-4cpu-diagnostic.json`, `vm-full-load-6cpu-diagnostic.json` | Failed/incomplete full-subscriber VM diagnostics; camera throughput remained below the 14-Hz acceptance threshold, and the 6-vCPU run failed source-timestamp freshness at 113.39 seconds |
| `vm-render-stages-diagnostic.json` | A later 4-vCPU full-subscriber diagnostic failed camera source freshness after 11.1 seconds (556.6 ms age); render-stage timing was captured after the subscriber stopped, not during that failure |
| `vm-post-discovery-fix.json` | After the agent discovery fixes, a 33.65-second VM producer interval delivered 500 Hz physics/native state, 200 Hz IMU and 14.41 FPS camera/observer with the original two-worker renderer; no full-sensor subscriber load |

The producer report's absolute queue counters include startup. Its
`snapshot_queue_delta` describes only the measured window. Nominal rates in
`compatibility.json` are interface targets, not substitutes for rate evidence.

Managed startup on WendyOS nightly-20260908T175754 (Linux 6.18.39) now passes
using the legacy IPv4/IPv6 firewall backend. This kernel has `CONFIG_NF_TABLES`
disabled; the updated agent loads the fixed required host modules before the
managed runtime starts. Both address families verify before DDS starts, and
the runtime then drops network administration capability. Named-VM agent update
and VM reboot followed by managed robot readiness also passed manual checks.
These lifecycle observations are separate from the container packet report.

Investigation of guest CPU load found an SPDP reply loop between Wendy RTPS
participants. The regression reproduced 6,513 discovery packets in 150 ms;
the fix completes the same exchange in three packets while preserving first
discovery, locator refresh and periodic announcements. The updated VM agent
also includes the concurrent host-network discovery fixes. Guest CPU use
dropped from roughly 89% to 45% in sampled four-vCPU readings. The post-update
producer report records the exact agent binary hash and retains the unchanged
runtime source and renderer settings.

The existing base-image agent needs an update that advertises
`go2-virtual-robot`. A binary from the matching checkout was used for the VM
checks; this does not establish availability in a published agent release.
The agent reports its OS as `wendyos`, and capability checks also require the
`vm-arm64` device type and explicit feature. See the [setup instructions](../README.md).

Remaining managed lifecycle acceptance includes user-app redeployment without
restarting the world, survival after the initiating terminal closes, crash and
explicit recovery, reconnect/profile continuity, typed inspection with another
ROS app, and two simultaneous VMs with independent sandbox ports and DDS buses.
Finite native and navigation checks do not establish sustained sensor throughput: **the ten-minute
VM gate has not passed**. The diagnostic reports retain bounded samples, not
complete continuous measurement logs.
