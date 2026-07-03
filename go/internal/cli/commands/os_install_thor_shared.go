package commands

// Cross-platform Jetson AGX Thor (T264) USB-recovery install flow. The stage-2
// partition flash (package flashengine) is shared across Windows, macOS and
// Linux; only stage-1 RCM boot, recovery-device enumeration, and the ADB
// transport differ per platform, provided by the thor*Host hooks implemented in
// os_install_thor_hw_{darwin_linux,windows}.go.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flashengine"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flashpack"
	"github.com/wendylabsinc/wendy/go/internal/cli/tui"
)

// Step IDs for the flashing progress list.
const (
	stepDownload = iota
	stepStage1
	stepStage2
)

// errGadgetUnreachable marks a stage-2 failure where the flashing gadget never
// appeared over USB. Nothing was written when this happens, so the Thor is
// untouched — the caller shows the calm "gadget unreachable" hint rather than the
// alarming "bad state" recovery box. The platform thorOpenGadget hooks wrap it.
var errGadgetUnreachable = errors.New("thor flashing gadget did not appear over USB")

// thorDevice identifies a selected Jetson in recovery mode, platform-neutrally.
// PathKey is the stable physical-location key used to re-find the device across
// the RCM→gadget re-enumeration; Label is a human description.
type thorDevice struct {
	PathKey string
	Label   string
}

// installThor flashes a Jetson AGX Thor over USB recovery: plan the flashpack,
// brief + confirm, prepare the host (Windows installs the WinUSB driver), pick
// the device, then run download → stage-1 RCM boot → stage-2 partition flash as a
// BuildKit-style step list. Stage-2 is the shared Go engine (flashengine) on all
// platforms.
func installThor(ctx context.Context, version string, nightly, force bool) error {
	cacheDir, err := osCacheDir()
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}

	plan, err := planThorFlashpack(cacheDir, version, nightly)
	if err != nil {
		return err
	}

	// Fail fast on a full disk, before the user starts cabling the Thor.
	if err := checkThorDiskSpace(cacheDir, plan); err != nil {
		return err
	}

	// Brief the user (Windows briefing includes the one-time WinUSB driver note),
	// then confirm before touching USB.
	if err := confirmThorReady(plan.version); err != nil {
		return err
	}

	// Prepare the host: Windows installs+trusts the WinUSB driver (UAC); macOS/
	// Linux stop any conflicting adb server that would claim the gadget.
	if err := thorPrepareHost(os.Stdout); err != nil {
		return err
	}

	dev, err := pickThorRecoveryDevice()
	if err != nil {
		return err
	}
	fmt.Printf("\n%s %s\n", tui.Dim("Target:"), tui.Device(dev.Label))

	if !force {
		fmt.Println()
		fmt.Println(tui.WarningMessage("This erases QSPI + internal NVMe on the Thor. This cannot be undone."))
		ok, err := tui.ConfirmNoDefaultDanger(fmt.Sprintf("Flash %s?", dev.Label))
		if errors.Is(err, tui.ErrCancelled) || (err == nil && !ok) {
			return ErrUserCancelled
		}
		if err != nil {
			return err
		}
	}

	// flashCtx aborts in-flight step work (the stage-2 engine) when the user
	// confirms a ctrl+c cancel in the steps UI.
	flashCtx, cancelFlash := context.WithCancel(ctx)
	defer cancelFlash()

	var (
		fp          *flashpack.Flashpack
		closeGadget func()
	)
	steps := []flashStep{
		{id: stepDownload, label: "Download flashpack", run: func(out io.Writer, detail func(string)) (bool, error) {
			resolved, cached, err := downloadAndExtractFlashpack(cacheDir, plan, detail)
			fp = resolved
			return cached, err
		}},
		{id: stepStage1, label: "Stage 1  RCM boot", run: func(out io.Writer, _ func(string)) (bool, error) {
			return false, thorStageOne(fp, dev, out)
		}},
		{id: stepStage2, label: "Stage 2  flash partitions",
			abortWarning: "Partitions are being written — aborting now can leave the Thor unbootable. Press ctrl+c again to abort anyway.",
			run: func(out io.Writer, detail func(string)) (bool, error) {
				transport, closer, err := thorOpenGadget(dev, out)
				if err != nil {
					return false, err
				}
				closeGadget = closer
				return false, flashengine.Run(flashCtx, transport, flashengine.Options{
					FlashImagesDir: filepath.Join(fp.FlashWorkspaceDir(), "flash-images"),
					NvddLocalPath:  filepath.Join(fp.BundleDir(), "unified_flash", "tools", "flashtools", "flash", "nvdd"),
					Out:            out,
					// USB-push progress, e.g. "38% · 6.9/18.1 GiB". Tracks
					// transfers only, so it pauses during device-side writes
					// and verification.
					Progress: throttledDetail(detail, byteProgress),
				})
			}},
	}

	failedID, err := runFlashSteps(fmt.Sprintf("Flashing WendyOS %s", plan.version), steps, cancelFlash)
	if closeGadget != nil {
		closeGadget()
	}
	if err != nil {
		switch {
		case errors.Is(err, tui.ErrCancelled):
			if failedID == stepStage2 {
				// The abort interrupted partition writes; the Thor can be left
				// unbootable exactly like a stage-2 failure. Show the recovery
				// guide instead of exiting silently.
				printThorBadStateHint(os.Stdout)
			}
			return ErrUserCancelled
		case thorIsUSBAccessErr(err):
			// Stage 1 re-opens the device (it can re-enumerate between the scan
			// and the flash); a denied re-open gets the same guidance as the scan.
			fmt.Println("\n" + usbAccessHintBox())
		case errors.Is(err, errGadgetUnreachable):
			// Stage-1 booted but the gadget never re-enumerated; nothing was written.
			printThorGadgetUnreachableHint(os.Stdout)
		case failedID == stepStage2:
			// Partitions were being written; the Thor can be left unbootable.
			printThorBadStateHint(os.Stdout)
		case failedID == stepStage1:
			fmt.Println()
			fmt.Println(tui.WarningMessage("RCM boot failed — the Thor wasn't modified. Re-enter recovery mode and try again."))
		}
		return err
	}

	fmt.Println(tui.SuccessMessage(fmt.Sprintf("Flashed WendyOS %s — power-cycle the Thor out of recovery to boot it. (press the right button once)", plan.version)))
	return nil
}

// ---- flashpack plan / download (cross-platform) ----

// thorFlashPlan is the resolved flashpack to install and whether it is cached.
type thorFlashPlan struct {
	version string
	cached  bool
	info    *thorFlashpackInfo
}

func planThorFlashpack(cacheDir, version string, nightly bool) (thorFlashPlan, error) {
	if version != "" && flashpackCached(cacheDir, version) {
		return thorFlashPlan{version: version, cached: true}, nil
	}
	info, err := getThorFlashpackInfo(version, nightly)
	if err != nil {
		if version != "" {
			return thorFlashPlan{}, fmt.Errorf("flashpack %s not in cache and manifest lookup failed: %w", version, err)
		}
		return thorFlashPlan{}, err
	}
	if flashpackCached(cacheDir, info.Version) {
		return thorFlashPlan{version: info.Version, cached: true}, nil
	}
	return thorFlashPlan{version: info.Version, info: info}, nil
}

func flashpackCached(cacheDir, version string) bool {
	if _, err := os.Stat(filepath.Join(cacheDir, flashpack.FlashpackName(version))); err == nil {
		return true
	}
	if _, err := os.Stat(flashpack.TarballCachePath(cacheDir, version)); err == nil {
		return true
	}
	return false
}

// thorExtractedFactor estimates the extracted flashpack tree from the compressed
// tarball size. Real flashpacks extract to ~2.1×; 2.5 leaves margin.
const thorExtractedFactor = 2.5

// thorFlashpackSpaceNeeded estimates the bytes about to be written under cacheDir:
// download + extraction, extraction only when the tarball is already cached, or 0
// when the extracted tree exists (or the size is unknown — never block on that).
func thorFlashpackSpaceNeeded(cacheDir string, plan thorFlashPlan) int64 {
	if _, err := os.Stat(filepath.Join(cacheDir, flashpack.FlashpackName(plan.version))); err == nil {
		return 0
	}
	if fi, err := os.Stat(flashpack.TarballCachePath(cacheDir, plan.version)); err == nil {
		return int64(float64(fi.Size()) * thorExtractedFactor)
	}
	if plan.info != nil && plan.info.SizeBytes > 0 {
		return int64(float64(plan.info.SizeBytes) * (1 + thorExtractedFactor))
	}
	return 0
}

// checkThorDiskSpace errors out early when the volume holding cacheDir doesn't
// have room for the flashpack download + extraction. Best-effort: an unknown
// size or an unreadable free-space figure never blocks the install.
func checkThorDiskSpace(cacheDir string, plan thorFlashPlan) error {
	needed := thorFlashpackSpaceNeeded(cacheDir, plan)
	if needed == 0 {
		return nil
	}
	avail, ok := diskAvailBytes(cacheDir)
	if !ok || avail >= needed {
		return nil
	}
	const gib = 1 << 30
	return fmt.Errorf(
		"not enough free disk space for WendyOS %s: needs about %.1f GiB in %s, but only %.1f GiB is free.\nFree up space (older downloads in %s can be deleted) and try again",
		plan.version, float64(needed)/gib, cacheDir, float64(avail)/gib, cacheDir)
}

func downloadAndExtractFlashpack(cacheDir string, plan thorFlashPlan, detail func(string)) (*flashpack.Flashpack, bool, error) {
	if !plan.cached {
		img := &imageInfo{DownloadURL: plan.info.URL, ImageSize: plan.info.SizeBytes, Version: plan.version}
		tmp, err := downloadImageInto(img, throttledDetail(detail, byteProgress))
		if err != nil {
			return nil, false, fmt.Errorf("downloading flashpack: %w", err)
		}
		if plan.info.Checksum != "" {
			detail("verifying")
			if err := verifySHA256(tmp, plan.info.Checksum); err != nil {
				os.Remove(tmp)
				return nil, false, err
			}
		}
		if err := os.Rename(tmp, flashpack.TarballCachePath(cacheDir, plan.version)); err != nil {
			os.Remove(tmp)
			return nil, false, fmt.Errorf("caching flashpack: %w", err)
		}
	}
	detail("extracting")
	fp, err := flashpack.Resolve(cacheDir, plan.version)
	return fp, plan.cached, err
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing %s: %w", filepath.Base(path), err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("flashpack checksum mismatch: got %s, manifest says %s", got, want)
	}
	return nil
}

// ---- flashing progress UI (cross-platform) ----

type flashStep struct {
	id    int
	label string
	// abortWarning, when set, arms the steps UI's ctrl+c guard while this step
	// runs: the first ctrl+c shows the warning and only a second press within a
	// few seconds actually cancels. Steps without it keep instant cancel.
	abortWarning string
	run          func(out io.Writer, detail func(string)) (cached bool, err error)
}

func runFlashSteps(title string, steps []flashStep, cancelWork func()) (int, error) {
	if !isInteractiveTerminal() {
		return runFlashStepsPlain(title, steps)
	}
	m := tui.NewStepsModel(title)
	prog := tui.NewProgressProgram(m)
	type outcome struct {
		failedID int
		err      error
		verbose  []byte
	}
	// curID tracks the step being run so a ctrl+c cancel can report which step
	// was interrupted (the goroutine keeps running briefly after the UI exits).
	var curID atomic.Int32
	curID.Store(-1)
	resC := make(chan outcome, 1)
	go func() {
		failedID, runErr := -1, error(nil)
		var failVerbose []byte
		for _, s := range steps {
			buf := &boundedBuffer{max: maxRawBuildCapture}
			curID.Store(int32(s.id))
			prog.Send(tui.StepAbortGuardMsg{Warning: s.abortWarning})
			prog.Send(tui.StepStartMsg{ID: s.id, Label: s.label})
			cached, err := s.run(buf, func(d string) { prog.Send(tui.StepDetailMsg{ID: s.id, Detail: d}) })
			if err != nil {
				prog.Send(tui.StepFailMsg{ID: s.id})
				failedID, runErr, failVerbose = s.id, err, buf.Bytes()
				break
			}
			prog.Send(tui.StepDoneMsg{ID: s.id, Cached: cached})
		}
		resC <- outcome{failedID, runErr, failVerbose}
		prog.Send(tui.StepsDoneMsg{Err: runErr})
	}()
	final, uiErr := prog.Run()
	if uiErr != nil {
		return -1, fmt.Errorf("flash progress UI: %w", uiErr)
	}
	if cancelErr := final.(tui.StepsModel).Err(); errors.Is(cancelErr, tui.ErrCancelled) {
		// Abort the in-flight step (cancels the stage-2 engine's context) and
		// reap the worker so nothing outlives the CLI. Bounded: a step that
		// ignores cancellation (e.g. stage-1 USB ops) shouldn't wedge the exit.
		cancelWork()
		select {
		case <-resC:
		case <-time.After(10 * time.Second):
			fmt.Fprintln(os.Stderr, "warning: the flash worker didn't stop within 10s; temp files may be left behind")
		}
		return int(curID.Load()), cancelErr
	}
	res := <-resC
	if res.err != nil {
		os.Stdout.Write(res.verbose)
	}
	return res.failedID, res.err
}

func runFlashStepsPlain(title string, steps []flashStep) (int, error) {
	fmt.Println(title)
	for _, s := range steps {
		fmt.Printf("==> %s\n", s.label)
		cached, err := s.run(os.Stdout, func(string) {})
		if err != nil {
			return s.id, err
		}
		if cached {
			fmt.Println("    (cached)")
		}
	}
	return -1, nil
}

func throttledDetail(detail func(string), format func(downloaded, total int64) string) func(downloaded, total int64) {
	var lastNanos atomic.Int64
	const minInterval = int64(66 * time.Millisecond)
	return func(downloaded, total int64) {
		now := time.Now().UnixNano()
		prev := lastNanos.Load()
		if now-prev < minInterval {
			return
		}
		if !lastNanos.CompareAndSwap(prev, now) {
			return
		}
		detail(format(downloaded, total))
	}
}

// byteProgress formats transfer progress like "40% · 1.2/3.0 GiB". The percent
// is capped at 99 — completion is the step's ✓, and the stage-2 byte count can
// slightly overshoot its estimated total (push retries).
func byteProgress(written, total int64) string {
	const gib = 1 << 30
	if total <= 0 {
		return fmt.Sprintf("%.1f GiB", float64(written)/gib)
	}
	pct := min(written*100/total, 99)
	return fmt.Sprintf("%d%% · %.1f/%.1f GiB", pct, float64(written)/gib, float64(total)/gib)
}

// waitForThorRecovery handles an empty recovery scan: the user already confirmed
// the Thor is in recovery mode, so this usually means cabling or the button
// sequence needs another try. Explain what to check once, then rescan passively
// every 1.5s under a spinner until a Jetson appears — no keypress needed — or
// the user quits with q/ctrl+c. Always returns ≥1 device on success. Generic so
// both the gousb (rcm.RecoveryDevice) and WinUSB (winusb.Device) scans share it.
func waitForThorRecovery[T any](scan func() ([]T, error)) ([]T, error) {
	if !isInteractiveTerminal() {
		return nil, fmt.Errorf("no Jetson found in USB recovery mode")
	}

	fmt.Println()
	fmt.Println(tui.WarningMessage("No Jetson in USB recovery mode yet — it will be picked up automatically once it appears."))
	fmt.Println("  While this keeps scanning, double-check:")
	fmt.Println("   • the USB-C cable is in the " + briefPort.Render("port next to the HDMI port"))
	fmt.Println("   • the recovery button sequence: hold " + briefKey.Render("Force Recovery") + " (middle), tap " + briefKey.Render("Reset") + " (right), release")
	fmt.Println()

	p := tui.NewProgressProgram(tui.NewSpinner("Waiting for the Thor to appear... (press q to quit)"))
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				devs, err := scan()
				if err != nil {
					p.Send(tui.SpinnerDoneMsg{Err: err})
					return
				}
				if len(devs) > 0 {
					p.Send(tui.SpinnerDoneMsg{Result: devs})
					return
				}
			}
		}
	}()

	finalModel, err := p.Run()
	close(stop)
	if err != nil {
		return nil, fmt.Errorf("recovery scan: %w", err)
	}
	model := finalModel.(tui.SpinnerModel)
	if !model.Done() {
		return nil, ErrUserCancelled // q / ctrl+c
	}
	result, serr := model.Result()
	if serr != nil {
		return nil, serr
	}
	return result.([]T), nil
}

// readQuit reads a line and reports whether the user asked to quit (q/quit).
func readQuit() bool {
	var s string
	if _, err := fmt.Scanln(&s); err != nil {
		// Treat empty (bare Enter) as continue; EOF as quit.
		if errors.Is(err, io.EOF) {
			return true
		}
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "q", "quit":
		return true
	default:
		return false
	}
}

// ---- adb-server stop (used by the macOS/Linux host prep) ----

func stopConflictingADBServer() bool { return stopADBServer("127.0.0.1:5037") }

func stopADBServer(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte("0009host:kill")); err != nil {
		return false
	}
	_, _ = io.ReadFull(conn, make([]byte, 4))
	return true
}

// ---- hint boxes (cross-platform) ----

var (
	thorHintBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(tui.ColorError).
			Padding(1, 3)
	thorHintTitle = lipgloss.NewStyle().Foreground(tui.ColorError).Bold(true)
	thorHintEmph  = lipgloss.NewStyle().Foreground(tui.ColorNotice).Bold(true)
	thorHintCmd   = lipgloss.NewStyle().Foreground(tui.Sky500).Bold(true)
)

// printThorBadStateHint warns that an interrupted flash may have left the Thor
// booting only to the UEFI shell, and how to clear the rootfs slot-status marks.
func printThorBadStateHint(w io.Writer) {
	const guid = "781E084C-A330-417C-B678-38E696380CB9"
	cmdA := fmt.Sprintf("setvar RootfsStatusSlotA -guid %s -bs -rt -nv =0x00000000", guid)
	cmdB := fmt.Sprintf("setvar RootfsStatusSlotB -guid %s -bs -rt -nv =0x00000000", guid)
	body := strings.Join([]string{
		thorHintTitle.Render("⚠  Flashing was interrupted — the Thor may be in a bad state"),
		"",
		"Plug the Thor into a monitor and power-cycle it.",
		"If the screen shows " + thorHintEmph.Render("UEFI") + ", the Thor is in a bad state.",
		"",
		"Attach a USB keyboard and type these two commands exactly:",
		"",
		thorHintCmd.Render("  " + cmdA),
		thorHintCmd.Render("  " + cmdB),
		"",
		"Then power-cycle again, re-enter recovery mode, and re-run the flash.",
	}, "\n")
	fmt.Fprintln(w, "\n"+thorHintBorder.Render(body))
}

// printThorGadgetUnreachableHint is a calm note for a flash that never wrote data.
func printThorGadgetUnreachableHint(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, tui.WarningMessage("Couldn't reach the Thor's flashing gadget over USB — nothing was written, so the Thor is safe."))
	fmt.Fprintln(w, tui.Dim("  Unplug/replug the USB-C cable (the port next to HDMI), re-enter recovery mode, and flash again."))
}

const (
	usbUdevRulePath = "/etc/udev/rules.d/70-wendy-jetson.rules"
	usbUdevRule     = `SUBSYSTEM=="usb", ATTRS{idVendor}=="0955", MODE="0660", GROUP="plugdev", TAG+="uaccess"`
)

// usbAccessHintBox explains how to regain USB access to the Jetson when the OS
// refuses to open it: on Linux, udev permissions; on macOS, its own kernel driver
// bound to the recovery device (root needed to seize it).
func usbAccessHintBox() string {
	return thorHintBorder.Render(strings.Join(usbAccessHintLines(runtime.GOOS), "\n"))
}

// usbAccessHintLines builds the body of the USB-access-denied hint for the given
// GOOS. Split out from usbAccessHintBox so both OS branches are testable without
// depending on the runner's platform.
func usbAccessHintLines(goos string) []string {
	lines := []string{
		thorHintTitle.Render("⚠  USB access denied"),
		"",
		"A Jetson is in recovery mode, but the OS refused wendy access to its USB device.",
	}
	if goos == "linux" {
		return append(lines,
			"Grant access with a udev rule (one-time; the wendy deb/rpm packages install it):",
			"",
			thorHintCmd.Render("  echo '"+usbUdevRule+"' \\"),
			thorHintCmd.Render("    | sudo tee "+usbUdevRulePath),
			thorHintCmd.Render("  sudo udevadm control --reload-rules && sudo udevadm trigger"),
			"",
			"Add your user to the "+thorHintEmph.Render("plugdev")+" group — if your distro has none, create it",
			"("+thorHintCmd.Render("sudo groupadd plugdev && sudo usermod -aG plugdev $USER")+", then log in",
			"again) — replug the USB-C cable and rescan. Or re-run the flash with sudo.",
		)
	}
	// macOS: the device enumerated but couldn't be claimed. macOS binds its own
	// driver to the recovery device (often re-matched after a failed RCM boot), so
	// wendy needs root to seize it — lead with the two fixes that actually work.
	return append(lines,
		"It enumerated but couldn't be claimed: macOS binds its own driver to the",
		"recovery device (often re-matched after a failed RCM boot), so wendy needs",
		"root to seize it. Try, in order:",
		"",
		"  "+thorHintEmph.Render("•")+" Re-run the flash with "+thorHintCmd.Render("sudo")+".",
		"  "+thorHintEmph.Render("•")+" Or unplug the USB-C cable, re-enter recovery mode, and flash again.",
		"",
		"If another process holds it (a VM with USB passthrough, another flashing",
		"tool, or another wendy), quit that first.",
	)
}
