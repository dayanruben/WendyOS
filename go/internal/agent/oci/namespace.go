package oci

import "fmt"

// JoinGroupNamespaces modifies spec so the container joins the Linux namespaces
// owned by the primary container (identified by primaryPID).
//
// isolation controls which namespaces are shared:
//   - "shared-network": joins network and uts namespaces
//   - "shared-ipc":     also joins ipc namespace (enables /dev/shm sharing for ROS2/dora-rs)
//   - anything else:    no-op
func JoinGroupNamespaces(spec *Spec, primaryPID uint32, isolation string) error {
	if spec.Linux == nil {
		return fmt.Errorf("JoinGroupNamespaces: spec.Linux is nil")
	}
	if primaryPID == 0 {
		return fmt.Errorf("JoinGroupNamespaces: primaryPID must be non-zero")
	}

	join := map[string]bool{}
	switch isolation {
	case "shared-ipc":
		join["ipc"] = true
		join["network"] = true
		join["uts"] = true
	case "shared-network":
		join["network"] = true
		join["uts"] = true
	default:
		return nil
	}

	for i, ns := range spec.Linux.Namespaces {
		if join[ns.Type] {
			spec.Linux.Namespaces[i].Path = fmt.Sprintf("/proc/%d/ns/%s", primaryPID, ns.Type)
		}
	}
	return nil
}
