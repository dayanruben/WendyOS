package commands

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// hostBuildArch is the CPU architecture this machine runs local image builds
// on natively.
func hostBuildArch() string { return nativeHostArch(runtime.GOARCH, rosettaTranslated()) }

// nativeHostArch maps the CLI binary's GOARCH to the machine's. An amd64 CLI
// that Rosetta translates runs on an Apple silicon Mac, whose Docker VM and
// builds are arm64.
func nativeHostArch(goarch string, rosetta bool) string {
	if rosetta && goarch == "amd64" {
		return "arm64"
	}
	return goarch
}

// normalizeBuildArch maps the architecture spellings found in platforms,
// wendy.json and uname to GOARCH names; "" when arch is empty.
func normalizeBuildArch(arch string) string {
	switch a := strings.ToLower(strings.TrimSpace(arch)); a {
	case "x86_64", "x86-64", "x64":
		return "amd64"
	case "aarch64":
		return "arm64"
	case "armhf", "armv7", "armv7l", "armv6", "armv6l":
		return "arm"
	default:
		return a
	}
}

// platformBuildArch returns the normalized architecture of an "os/arch" or
// "os/arch/variant" platform, or "" when platform names no architecture.
func platformBuildArch(platform string) string {
	parts := strings.Split(platform, "/")
	if len(parts) < 2 {
		return ""
	}
	return normalizeBuildArch(parts[1])
}

// emulatedBuildNotice returns the notice for a local build of platform on a
// hostOS/hostArch machine, or "" when the build runs natively or either
// architecture is unknown. buildHostSupported says whether this project could
// use --build-host instead: remote builds take single-service projects only.
//
// hostOS matters for exactly one pair: an arm64 host building a 32-bit arm
// (AArch32) target. Everywhere except Apple silicon, the kernel runs AArch32
// binaries natively through compat mode, so there is no QEMU and no benefit
// to --build-host; Apple silicon has no AArch32 support at all, so that
// build really does run under QEMU there.
func emulatedBuildNotice(hostOS, hostArch, platform string, buildHostSupported bool) string {
	host := normalizeBuildArch(hostArch)
	target := platformBuildArch(platform)
	if host == "" || target == "" || host == target {
		return ""
	}
	if host == "arm64" && target == "arm" && hostOS != "darwin" {
		return ""
	}
	msg := fmt.Sprintf("Building %s on this %s machine: RUN steps run under QEMU emulation, often several times slower than native.", platform, host)
	if buildHostSupported {
		return msg + fmt.Sprintf(" Use --build-host=DEVICE to build on a native %s WendyOS device instead.", target)
	}
	return msg + " (--build-host, which builds on a WendyOS device, supports single-service projects only.)"
}

// emulatedBuildNoticeOnce limits the notice to one per process: `wendy watch`
// rebuilds on every change, and a multi-service group builds many images.
var emulatedBuildNoticeOnce sync.Once

// noteEmulatedBuild prints emulatedBuildNotice once per process, just before
// a local image build of platform. Call it only once the build will really
// run here: after the no-build fast paths, and never for --build-host.
func noteEmulatedBuild(platform string, buildHostSupported bool) {
	noteEmulatedBuildWith(&emulatedBuildNoticeOnce, runtime.GOOS, hostBuildArch(), platform, buildHostSupported, func(msg string) { cliNotice("%s", msg) })
}

func noteEmulatedBuildWith(once *sync.Once, hostOS, hostArch, platform string, buildHostSupported bool, print func(string)) {
	msg := emulatedBuildNotice(hostOS, hostArch, platform, buildHostSupported)
	if msg == "" {
		return // a native build does not use up the once
	}
	once.Do(func() { print(msg) })
}
