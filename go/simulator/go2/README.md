# Go2 virtual robot for Wendy Simulator

A Go2 profile turns a WendyOS VM into a robot development target. A managed
container runs MuJoCo, a pinned ONNX locomotion policy, ROS 2 Humble/CycloneDDS,
and a browser sandbox. Applications in that VM receive causal robot observations
and send ROS commands that move a freely walking, contact-supported robot.
Inference and rendering use the CPU; the profile defaults to four vCPUs and
4 GiB RAM on accelerated ARM64.

## Create or configure a simulator

Use a CLI built from this checkout. In the Simulator tab, create a simulator
and select **Unitree Go2**, then connect to it. The first connection builds the
runtime from pinned sources, downloads verified assets, deploys it, and waits
for robot readiness. Docker must be available on the development host.

The agent must advertise `go2-virtual-robot` support. Older VM images get an
explicit update instruction before any runtime build. Agent maintenance stays
available through the named VM even when robot startup fails. During development,
use an agent built from the same checkout: `make build-agent-linux-arm64`, then
`wendy --device vm:go2-sim device update --binary bin/wendy-agent-linux-arm64`.
A published supporting agent release can use ordinary `device update`.

The diagnostic CLI supports the same workflow:

```sh
wendy vm create go2-sim --profile go2
wendy vm robot start go2-sim
wendy vm robot open go2-sim
```

For an existing ordinary VM, use `wendy vm robot configure <name>` before
`wendy vm robot start <name>`. Configuration preserves the VM disk and existing
applications. Each VM gets its own verified loopback sandbox URL; use
`wendy vm robot status <name>` to find it. Do not assume a shared host port.

The persisted profile pins the runtime source and policy bundle. A newer CLI
reports a source mismatch until `wendy vm robot update <name>` is explicitly
requested. Reconnecting preserves a running world's state; a stopped runtime
is restarted. `wendy vm robot restart <name>` explicitly recovers a failed
runtime. `wendy vm robot reset <name>` resets the world and revokes command
ownership. Stopping a user application leaves the managed robot running.

## Deploy a ROS application

Declare ROS in the application's `wendy.json`:

```json
{
  "frameworks": {
    "ros2": {
      "distro": "humble",
      "rmw": "rmw_cyclonedds_cpp",
      "domainId": 0,
      "discoveryScope": "app"
    }
  }
}
```

Run `wendy --device vm:go2-sim run` from the application project. Wendy resolves
declared ROS applications onto the profile's guest host network and loopback
ROS bus, checking conflicting domain, middleware and discovery settings before
deployment. Source manifests are not rewritten. The managed runtime installs
guest firewall rules that confine UDP RTPS to loopback, then drops its network
administration capability. Application downloads and ordinary networking work.
Independent VMs have independent robot buses.

Kernels without nftables use verified legacy IPv4/IPv6 filters. That fallback
blocks UDP packets containing the `RTPS` marker anywhere, independent of DDS
domain and port; it can also block unrelated UDP carrying those bytes. The
agent loads fixed filter modules only for a validated managed Go2 runtime on a
WendyOS VM. Both address families must be protected before DDS starts.

Standard applications publish `geometry_msgs/msg/Twist` on `/cmd_vel`.
Start the publisher with a zero command, open the sandbox, select its ROS
command source, and choose **Give app control**. Exactly one browser or DDS
publisher owns actuation. A grant is required for native sport and LowCmd
control too. The runtime discovers the actual middleware publisher identity;
it does not guess ownership from a node name.

| Observations | Nominal rate |
| --- | --- |
| `/lowstate`, `/lf/lowstate` | 500 / 50 Hz |
| `/sportmodestate`, `/lf/sportmodestate` | 50 Hz in sport mode |
| `/imu/data`, `/utlidar/imu` | 200 Hz |
| `/joint_states`, `/odom`, `/simulation/ground_truth` | 50 Hz |
| `/scan`, `/utlidar/cloud` | 10 Hz |
| `/camera/color/image_raw`, `/camera/color/camera_info` | 15 Hz, 640×360 RGB |

`/tf` supplies `odom → base_link`; `/tf_static` supplies the IMU, lidar,
camera and optical mounting transforms. A localization node can own
`map → odom`. Device mode uses original wall-clock capture timestamps and
does not publish `/clock`; applications use `use_sim_time=false`.

Native interfaces use generated `unitree_go` and `unitree_api` types. The
supported sport subset includes version queries, Move, StopMove, BalanceStand,
physical StandUp/StandDown, and Damp. LowCmd supports twelve active motors in
the twenty-slot SDK layout, CRC checks, stop sentinels and exclusive external
joint control. Unsupported APIs return explicit errors. Native applications
must include their SDK or message dependencies; Wendy's typed ROS inspector
uses the runtime's pinned overlay.

## Sandbox and command lifetime

Choose **Enable controls**, then hold W/S to walk, A/D to strafe, or Q/E to
turn. Space stops the velocity target. The browser offers independent sandbox
and robot camera views, an adjustable obstacle, and lidar/camera fault controls.
Sensor pauses stop new samples while physics continues; lidar dropout applies
the same seeded mask to scan and cloud. Fault settings persist across world reset.

Velocity commands expire after 200 ms and are acceleration-limited to
0.8 m/s forward, 0.5 m/s lateral and 1 rad/s yaw. External LowCmd expires after
40 ms and enters damping. It never falls back into autonomous walking.
Pause and reset revoke all grants and reject publishers from the previous
world. Restart the command publisher and explicitly grant it again. Resume
does not rearm controls. Fallen robots require reset.

HTTP endpoints are `/api/health`, `/api/status`, `/api/profile`, `/frame.jpg`
and `/camera.jpg`. Status reports the simulation identity, source digest,
control owner, world epoch, sensor settings and measured performance. A ready
agent connection alone does not mean the robot is ready.

## Compatibility and validation

The versioned [compatibility manifest](compatibility.json) describes exact
types, nominal rates, command IDs, error codes and fidelity limits. It is also
served at `/api/profile`. [UPSTREAM.md](UPSTREAM.md) records model, policy,
message definitions, derived wire-layout fixes, visual assets and licenses.

Joint, inertial, contact, camera and lidar observations derive from MuJoCo.
Odometry integrates velocity with a declared bias and nonzero covariance;
exact pose is a separate simulation topic. The lidar pattern and camera
mount are virtual attachments. Battery telemetry is a documented constant
synthetic model. Factory gait parity, calibrated sensors, depth, native video
codecs, motion-switcher mutations, and cloud/audio/AI services are unsupported.

Development checks, from this directory:

```sh
python3 tools/fetch_assets.py
python3 tools/fetch_assets.py --check
python3 tools/fetch_unitree.py
python3 tools/prepare_unitree.py
uv venv --python 3.12 .venv
uv pip install --python .venv/bin/python -r requirements-dev.txt
.venv/bin/python -m pytest -q
docker build -t wendy-go2-managed:dev .
```

The production Dockerfile generates and verifies visual assets on Linux ARM64
and builds the derived ROS overlay. Original physics assets remain unchanged.
Use the container for software rendering; native macOS OpenGL is not the
supported rendering path.

Separate applications exercise real DDS delivery and commands:

- [Standard ROS and VM soak](integration/standard/README.md): signed walking,
  sensors, command expiry, reset rejection, and a ten-minute performance gate.
- [Native SDK acceptance](integration/native/README.md): actual SDK requests,
  stock ROS decoding, motor ordering, CRC and low-level watchdog behavior.
- [Navigation acceptance](integration/navigation/README.md): goal arrival,
  obstacle stopping and stale-scan behavior through a small reactive controller.
- [DDS isolation](integration/isolation/README.md): IPv4/IPv6 packet delivery,
  loopback access, firewall ownership and capability removal.

Recorded results live in `validation/`; producer timing reproduction is in
[tools/ros_timing.md](tools/ros_timing.md). Nominal rates and local producer
benchmarks do not by themselves establish the deployed VM performance gate.
The [implementation plan](../../docs/plans/2026-09-12-go2-virtual-robot.md)
tracks remaining acceptance work. No physical robot is used for these tests.
