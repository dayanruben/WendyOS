package containerd

import "github.com/wendylabsinc/wendy/go/internal/agent/services"

const (
	go2RuntimeAppID    = "sh.wendy.simulator.go2"
	labelKeyGo2Overlay = "sh.wendy/ros2.go2.overlay"
	go2OverlayVersion  = "1"
	go2ROSSetup        = "/opt/wendy-go2/ros_ws/install/setup.sh"
)

// The managed runtime carries the Unitree types. A random user app may carry
// only standard ROS messages, so prefer this anchor whenever it is running.
// Namespace ownership and task lifetime are still verified by the normal path.
func isGo2ROS2Target(target *services.ROS2Target) bool {
	return target != nil && target.Running && target.AppID == go2RuntimeAppID &&
		target.Distro == "humble" && target.DomainID == 0 &&
		(target.RMW == "rmw_cyclonedds_cpp" || target.RMW == "")
}

func preferROS2Anchor(candidate, current *services.ROS2Target) bool {
	return current == nil || (isGo2ROS2Target(candidate) && !isGo2ROS2Target(current))
}

// Both paths are fixed build-time values. No app-supplied path or shell text is
// sourced. The standalone host inspector keeps its existing read-only path.
func ros2SourceAndExecForOverlay(distro string, go2 bool) string {
	if !go2 {
		return ros2SourceAndExec(distro)
	}
	return ". " + ros2SetupScript(distro) + " >/dev/null 2>&1 && . " +
		go2ROSSetup + " >/dev/null 2>&1 && exec ros2 \"$@\""
}
