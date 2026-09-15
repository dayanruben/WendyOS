# Go2 roaming example

This small application walks the virtual Go2 around its room, stops before
obstacles, and turns toward open space using lidar and odometry. **Start** and
**Stop** control the roaming behavior. The controller limits its velocity
requests and stops when observations become stale or incomplete. Fresh sensor
data alone cannot restart a stopped controller.

Use this example with the Wendy Go2 simulator. It is a reactive obstacle-avoidance
example; it does not build a map or plan routes.

## Run beside the local browser preview

From this directory, with the simulator running at `http://127.0.0.1:8898`:

```sh
python3 app.py --simulator http://127.0.0.1:8898
```

Open `http://127.0.0.1:8901` for the application's Start/Stop controls, status,
and embedded 3D sandbox. Use **Follow robot** to keep it in view, or drag to
orbit and pan while it explores.
Start the application explicitly when ready to roam. Start acquires this local
application's simulator control lease; Stop requests zero velocity and releases
it, leaving the simulator available for inspection. `--port` changes the
application page's port if 8901 is already in use.

If manual controls already own the robot, choose **Release controls** in the
original sandbox tab before starting. If that tab is no longer available,
**Pause** then **Resume** in the sandbox releases the old owner. Then choose
**Start exploring** in the application.

## Deploy the ROS application to a Go2 VM

The ROS adapter reads `/scan` (`sensor_msgs/msg/LaserScan`) and `/odom`
(`nav_msgs/msg/Odometry`), then publishes velocity requests on `/cmd_vel`
(`geometry_msgs/msg/Twist`) at 20 Hz. It uses the same controller as the local
application. Its observations and commands travel through ROS; the deployed
image contains no simulator or physics implementation.

From this directory:

```sh
wendy run --device vm:<simulator-name> --build-type docker --no-restart
```

The supplied manifest selects ROS 2 Humble, CycloneDDS, domain 0 and the Go2 VM's
loopback ROS bus. The Docker image runs `ros_app.py --autostart`, which starts the
controller once fresh lidar and odometry arrive. Open the simulator sandbox,
select this application's source under **ROS command source**, and choose
**Give app control**. Velocity requests can move the robot only after that grant.

In a ROS-enabled shell on the same VM bus, use the Trigger services to control
the behavior and inspect its status:

```sh
ros2 service call /roam/start std_srvs/srv/Trigger '{}'
ros2 service call /roam/stop std_srvs/srv/Trigger '{}'
ros2 topic echo /roam/status std_msgs/msg/String
```

Without `--autostart`, `python3 ros_app.py` publishes zero velocity until a
successful `/roam/start` request. A start request needs current observations.
Status includes the controller state, stop reason, clearance, velocity requests
and sensor ages. `/roam/stop` immediately requests zero velocity; **Release app
control** in the simulator also removes the application's command ownership.

The adapter accepts scans in `lidar_link` and odometry from `odom` to
`base_link`. It rejects invalid poses, wrong frames, reordered observations and
source timestamps older than 350 ms. Delayed observations retain their capture
age. Missing or unknown lidar coverage causes the controller to stop.

After stale observations, issue a new Start request once sensors recover.
After a simulator pause or world reset, restart the ROS application and explicitly
grant its newly discovered publisher again. Resuming the simulator alone does
not restore the old grant. The ROS adapter never grants itself control.

## Check the controller, local runner and ROS observation admission

From this directory, using the simulator's development environment:

```sh
../../.venv/bin/python -m pytest -q test_controller.py test_app.py test_ros_app.py
```

These checks run without ROS. The deployed adapter uses the Humble packages
installed by the Dockerfile and needs no additional pip dependencies.
