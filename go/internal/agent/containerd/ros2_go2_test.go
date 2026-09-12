package containerd

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/agent/services"
)

func TestGo2ROS2AnchorRequiresRunningMatchingProfile(t *testing.T) {
	base := services.ROS2Target{AppID: go2RuntimeAppID, Distro: "humble", DomainID: 0,
		RMW: "rmw_cyclonedds_cpp", Running: true}
	ordinary := base
	ordinary.AppID = "example.navigation"
	if !preferROS2Anchor(&base, &ordinary) || preferROS2Anchor(&ordinary, &base) {
		t.Fatal("typed runtime must win independently of container listing order")
	}
	for name, change := range map[string]func(*services.ROS2Target){
		"stopped": func(v *services.ROS2Target) { v.Running = false },
		"domain":  func(v *services.ROS2Target) { v.DomainID = 42 },
		"distro":  func(v *services.ROS2Target) { v.Distro = "jazzy" },
		"rmw":     func(v *services.ROS2Target) { v.RMW = "rmw_fastrtps_cpp" },
		"app":     func(v *services.ROS2Target) { v.AppID = ordinary.AppID },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			change(&candidate)
			if isGo2ROS2Target(&candidate) || preferROS2Anchor(&candidate, &ordinary) {
				t.Fatal("unrelated/stopped target selected as managed Go2")
			}
		})
	}
}

func TestGo2ROS2OverlayShellPreservesArgumentBoundary(t *testing.T) {
	if got := ros2SourceAndExecForOverlay("humble", false); got != ros2SourceAndExec("humble") {
		t.Fatal("ordinary and host inspection shell changed")
	}
	script := ros2SourceAndExecForOverlay("humble", true)
	if !strings.Contains(script, ". "+go2ROSSetup+" >/dev/null 2>&1 && exec ros2 \"$@\"") {
		t.Fatalf("missing fixed overlay followed by quoted argv: %s", script)
	}
	// Exercise the actual shell suffix with harmless stand-ins for the two
	// fixed setup scripts. Argument content must never be reinterpreted.
	script = strings.ReplaceAll(script, ". "+ros2SetupScript("humble")+" >/dev/null 2>&1", "true")
	script = strings.ReplaceAll(script, ". "+go2ROSSetup+" >/dev/null 2>&1", "true")
	script = strings.Replace(script, "exec ros2", "printf '%s\\n'", 1)
	value := "/topic; printf INJECTED"
	cmd := exec.Command("sh", "-c", script, "ros2", "topic", "echo", value)
	got, err := cmd.CombinedOutput()
	if err != nil || string(got) != "topic\necho\n"+value+"\n" {
		t.Fatalf("argv boundary changed: %q, %v", got, err)
	}
}
