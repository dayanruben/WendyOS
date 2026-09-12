# Unitree G1 virtual robot

The `g1` simulator profile runs a free-floating, 29-joint Unitree G1 in MuJoCo
inside a WendyOS VM. Joint torques, contacts, and the pinned Unitree RL Lab
velocity policy produce locomotion. The sandbox supports browser control and
ROS 2 applications running in the same VM.

## Start from the CLI

From this repository, with Docker running:

```sh
make build-cli
./bin/wendy vm create my-g1 --profile g1 --nightly
./bin/wendy vm robot start my-g1
./bin/wendy vm robot open my-g1
```

The first start builds the runtime and downloads hash-verified model, policy,
and Unitree ROS interface sources. The profile requests four CPUs and 4 GiB of memory.
`vm robot status my-g1 --json` reports the verified `sandbox_url`; its host port
can differ between VMs.

If the VM's agent predates G1 support, install the agent from this checkout:

```sh
make build-agent-linux-arm64
./bin/wendy --device vm:my-g1 device update --binary bin/wendy-agent-linux-arm64
./bin/wendy vm robot start my-g1
```

For an existing ordinary VM, use
`./bin/wendy vm robot configure my-vm --profile g1`. An existing robot profile
cannot be replaced by this command. Use `vm robot update my-g1` to apply a newer
G1 runtime from a rebuilt CLI. Go2 and G1 retain separate source pins and app IDs.

## ROS applications

Deploy a ROS 2 Humble application with `./bin/wendy run --device vm:my-g1`.
The managed profile uses CycloneDDS, domain zero, and guest loopback networking.
Start its command publisher, then select that source in the sandbox and click
**Give app control**. Browser controls and ROS publishers have one exclusive owner.

The learned policy initiates reliably around 0.3 m/s. Small commands from rest
and turning in place track weakly; use forward walking with yaw for a turn.
No command amplification or artificial root movement is applied.

Supported inputs are `/cmd_vel` (`geometry_msgs/msg/Twist`), the pinned G1
`LocoClient` velocity/FSM subset on `/api/sport/request`, and PR-mode
`unitree_hg/msg/LowCmd` on `/lowcmd`. G1 native messages have 35 motor slots,
with 29 physical joints in Unitree motor order. They use the HG wire layout and
CRC, which differ from Go2.

Outputs include native HG motor/IMU state, standard IMU and joint states,
odometry, transforms, a five-ring lidar point cloud and horizontal scan, RGB
camera images and calibration, plus separate ground-truth pose and reset epoch.
The browser has an orbitable 3D scene and the robot's sensor camera.

High-level commands expire after 200 ms; low-level commands expire after 40 ms
and enter damping. Continuously publish motion commands. Reset and pause revoke
control and require restarting the command publisher before granting it again.
This prevents queued or continuing commands from controlling a reset robot.

## Fidelity and limits

This is a documented development profile, with an open locomotion policy and
virtual camera/lidar mounts. It does not reproduce Unitree firmware or factory
calibration. Arms participate in the 29-joint policy. Arm, audio, trajectory,
and factory posture services are not emulated. The implemented native services
return explicit errors for unsupported command IDs. Native low-level control
requires the application to stabilize the humanoid.

Both visual settings retain the pinned mesh topology. `balanced` disables
expensive camera rendering effects. The browser renders its own orbit view.
Sensor timestamps retain their actual wall-clock capture time; paused physics
produces no fresh observations. Odometry has documented integration bias and
covariance; `/simulation/ground_truth` contains exact simulation pose.

See [compatibility.json](compatibility.json) for exact topics, command IDs,
motor layout, rates, and declared limitations, and [UPSTREAM.md](UPSTREAM.md)
for the immutable source revisions, licenses, and policy observation mapping.
The [validation reports](validation/README.md) record the 76 Linux checks,
independent SDK acceptance, and measured ROS motion and sensor rates in a VM.

## Local development

```sh
cd simulator/g1
python3 -m venv .venv
.venv/bin/pip install -r requirements-dev.txt
.venv/bin/python tools/fetch_assets.py
.venv/bin/python -m pytest tests
.venv/bin/python -m g1_sim.server
```

The local server binds `127.0.0.1:8890`. It needs a working OpenGL backend for
the camera. The managed Linux image uses OSMesa. For the reproducible ROS image:

```sh
docker build -t wendy-g1-managed:dev .
```
