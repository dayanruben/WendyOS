# Go2 validation evidence

These reports distinguish functional compatibility, local producer timing and
actual deployed VM acceptance. All tests use virtual robots.

| Report | Scope |
| --- | --- |
| `native-sdk.json` | Actual pinned Unitree SDK and generated/stock ROS participants in isolated ARM64 Linux containers; commands, postures, LowState CRC/motor ordering and LowCmd watchdog |
| `navigation.json`, `navigation-source.json` | Separate ROS goal controller with camera/cloud subscriptions, obstacle and stale-scan scenarios; exact source and visual hashes recorded |
| `producer-timing.json`, `producer-timing-source.json` | 30-second local producer benchmark after warm-up; both render views, native state and lidar enabled; no external subscriber load |

The producer report's absolute queue counters include startup. Its
`snapshot_queue_delta` describes only the measured window. Nominal rates in
`compatibility.json` are interface targets, not substitutes for rate evidence.

The managed VM startup test exposed that WendyOS nightly-20260908T175754's
Linux 6.18.39 kernel has `CONFIG_NF_TABLES` disabled. IPv4 legacy filtering and
its string matcher passed kernel dry-run checks. IPv6 filtering requires its
host kernel module to be loaded before the application starts. The runtime
fails closed until both families verify. The managed VM soak and complete
lifecycle gates are still pending; no ten-minute VM result is claimed here.
