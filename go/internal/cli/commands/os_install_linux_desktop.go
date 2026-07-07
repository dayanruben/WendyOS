//go:build darwin || linux || windows

package commands

import (
	"fmt"
	"strings"
	"time"
)

const (
	linuxDesktopValue    = "linux-desktop"
	linuxDesktopAgentURL = "https://install.wendy.dev/agent.sh"
)

// renderLinuxDesktopInstructions returns the text printed when the user picks
// "Linux Desktop". With an empty token it prints the plain (unenrolled) docs
// command; with a token it prints the pre-enrollment one-liner.
func renderLinuxDesktopInstructions(token, cloudHost, orgName string, expiresAt time.Time) string {
	var b strings.Builder
	if token == "" {
		fmt.Fprintf(&b, "Install wendy-agent on your Linux machine:\n\n")
		fmt.Fprintf(&b, "  curl -fsSL %s | bash\n\n", linuxDesktopAgentURL)
		fmt.Fprintf(&b, "The device is discovered over your local network — run `wendy discover`.\n")
		fmt.Fprintf(&b, "To enroll it into an org later, run `wendy device enroll`\n")
		fmt.Fprintf(&b, "(or re-run `wendy install` while logged in for a pre-enrollment token).\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Install wendy-agent on your Linux machine; it will enroll into %s automatically.\n\n", orgName)
	fmt.Fprintf(&b, "  curl -fsSL %s | \\\n", linuxDesktopAgentURL)
	fmt.Fprintf(&b, "    WENDY_ENROLLMENT_TOKEN=%s WENDY_CLOUD_HOST=%s bash\n\n", token, cloudHost)
	fmt.Fprintf(&b, "This enrollment token expires at %s (about 1 hour). Run the command before then.\n", expiresAt.Format(time.RFC1123))
	fmt.Fprintf(&b, "After it boots, run `wendy discover` to find the device.\n")
	return b.String()
}
