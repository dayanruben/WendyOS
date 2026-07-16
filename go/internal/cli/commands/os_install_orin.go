//go:build darwin || linux

package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/bringup"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/flashpack"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/rcm"
	"github.com/wendylabsinc/wendy/go/internal/cli/tegraflash/t234"
	"github.com/wendylabsinc/wendy/go/internal/cli/tui"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	"github.com/wendylabsinc/wendy/go/internal/shared/wendyconf"
)

const (
	orinStepDownload = iota
	orinStepProvision
	orinStepRCMBoot
	orinStepCommands
	orinStepWrite
	orinStepStatus
)

// installOrin performs a complete T234 recovery from a pre-signed schema-v2
// flashpack. No NVIDIA host binary or container is used on macOS/Linux.
func installOrin(ctx context.Context, opts t234InstallOptions) error {
	cacheDir, err := osCacheDir()
	if err != nil {
		return fmt.Errorf("resolving cache dir: %w", err)
	}
	if opts.Artifact == nil {
		return fmt.Errorf("missing recovery artifact metadata")
	}
	ref := flashpack.RecoveryRef{Device: opts.DeviceType, Storage: opts.Storage, Version: opts.Version}

	// Identity/verify/status files pass between this process and the root
	// __t234-write helper. Keep them in a private 0700 dir, not shared /tmp:
	// Linux fs.protected_regular denies root an O_CREAT open of a file this
	// unprivileged process owns in a sticky, world-writable dir.
	handoffDir, err := os.MkdirTemp("", "t234-flash-")
	if err != nil {
		return fmt.Errorf("creating flash temp dir: %w", err)
	}
	defer os.RemoveAll(handoffDir)

	creds, err := resolveWiFiCredentialsList(opts.WiFi)
	if err != nil {
		return err
	}
	name, err := resolveDeviceName(opts.RequestedName)
	if err != nil {
		return err
	}
	provJSON, err := resolveProvisioningJSON(ctx, opts.PreEnroll, name)
	if err != nil {
		return err
	}

	if err := confirmOrinReady(opts); err != nil {
		return err
	}
	dev, err := pickUnixRecoveryDevice(orinRecoveryHints(opts), func(d rcm.RecoveryDevice) bool { return d.IsOrin() })
	if err != nil {
		return err
	}
	fmt.Printf("\n%s %s\n", tui.Dim("Target:"), tui.Device(dev.Describe()))

	if !opts.Force {
		fmt.Println()
		fmt.Println(tui.WarningMessage(fmt.Sprintf("This erases QSPI and all data on the selected %s, including /data. This cannot be undone.", strings.ToUpper(opts.Storage))))
		ok, err := tui.ConfirmNoDefaultDanger(fmt.Sprintf("Flash %s?", dev.Describe()))
		if errors.Is(err, tui.ErrCancelled) || (err == nil && !ok) {
			return ErrUserCancelled
		}
		if err != nil {
			return err
		}
	}

	if err := preAuthElevation(); err != nil {
		return err
	}
	elevationCtx, cancelElevation := context.WithCancel(ctx)
	defer cancelElevation()
	keepElevationAlive(elevationCtx)
	flashCtx, cancelFlash := context.WithCancel(ctx)
	defer cancelFlash()

	var logW io.Writer = io.Discard
	var logPath string
	if dir, derr := config.LogDir(); derr == nil {
		logPath = filepath.Join(dir, "orin-recovery-"+time.Now().Format("20060102-150405")+".log")
		if lf, lerr := os.Create(logPath); lerr == nil {
			defer lf.Close()
			fmt.Fprintf(lf, "wendy os install — %s %s recovery — WendyOS %s\n\n", opts.DeviceType, opts.Storage, opts.Version)
			logW = lf
			defer func() { fmt.Println(tui.Dim("Full flash log: " + logPath)) }()
		} else {
			logPath = ""
		}
	}

	var (
		fp          *flashpack.Flashpack
		workspace   string
		layoutPath  string
		flashPlan   *t234.Plan
		massStorage *t234.Stage2
		finalStatus *t234.FinalStatus
	)
	defer func() {
		if workspace != "" {
			_ = os.RemoveAll(workspace)
		}
	}()

	boundaryWarning := "Recovery data has been handed to the Jetson — aborting now can leave QSPI or the rootfs partially written. Press ctrl+c again to abort anyway."
	steps := []flashStep{
		{id: orinStepDownload, label: "Download recovery flashpack", run: func(out io.Writer, detail func(string)) (bool, error) {
			var cached bool
			fp, cached, err = resolveT234Flashpack(cacheDir, ref, opts.Artifact, detail)
			return cached, err
		}},
		{id: orinStepProvision, label: "Prepare per-run config", run: func(out io.Writer, detail func(string)) (bool, error) {
			workspace, layoutPath, err = prepareT234Workspace(fp)
			if err != nil {
				return false, err
			}
			configRel, err := filepath.Rel(fp.FlashImagesDir(), fp.ConfigImage())
			if err != nil {
				return false, err
			}
			if err := injectRecoveryConfig(filepath.Join(workspace, configRel), creds, name, provJSON, out, detail); err != nil {
				return false, err
			}
			flashPlan, err = t234.LoadXMLPlan(layoutPath, workspace, fp.Manifest.RootfsDevice)
			if err != nil {
				return false, err
			}
			target := fp.Manifest.Target
			massStorage = &t234.Stage2{
				FlashPackagePath: fp.FlashPackageImage(), LayoutPath: layoutPath, ImagesDir: workspace,
				Plan: flashPlan, PortPath: dev.PathKey,
				StatusPath: fp.Manifest.Layout.FlashPackageStatus, LogsPath: fp.Manifest.Layout.FlashPackageLogs,
				ExpectedIdentity: t234.IdentityExpectation{ModuleID: target.ModuleID, ModuleSKU: target.ModuleSKU, CarrierID: target.CarrierID, CarrierSKU: target.CarrierSKU},
				Out:              out, Detail: detail, RunHelper: runT234Helper, TempDir: handoffDir,
			}
			return false, nil
		}},
		{id: orinStepRCMBoot, label: "Stage 1  RCM boot", run: func(out io.Writer, _ func(string)) (bool, error) {
			order, memBCT, blob, err := t234RCMFiles(fp)
			if err != nil {
				return false, err
			}
			return false, bringup.Run(bringup.Options{Dir: fp.Root, MemBCT: memBCT, Blob: blob, DevicePath: dev.PathKey,
				ExpectedProduct: uint16(rcm.ProductOrin), SendOrder: order, Out: out})
		}},
		{id: orinStepCommands, label: "Stage 2  verify target + hand off recovery", abortWarning: boundaryWarning, run: func(out io.Writer, detail func(string)) (bool, error) {
			massStorage.Out, massStorage.Detail = out, detail
			return false, massStorage.SendFlashPackage(flashCtx)
		}},
		{id: orinStepWrite, label: "Stage 2  write " + strings.ToUpper(opts.Storage) + " partitions", abortWarning: boundaryWarning, run: func(out io.Writer, detail func(string)) (bool, error) {
			massStorage.Out, massStorage.Detail = out, detail
			err := massStorage.WriteRootfsDevice(flashCtx)
			if errors.Is(err, t234.ErrDeviceSideFailed) {
				finalStatus, _ = massStorage.AwaitFinalStatus(flashCtx)
				if finalStatus != nil {
					saveOrinDeviceLogs(out, logPath, finalStatus)
					return false, fmt.Errorf("device failed before rootfs export: status %q", finalStatus.Status)
				}
			}
			return false, err
		}},
		{id: orinStepStatus, label: "Stage 2  collect final device status", abortWarning: boundaryWarning, run: func(out io.Writer, detail func(string)) (bool, error) {
			massStorage.Out, massStorage.Detail = out, detail
			finalStatus, err = massStorage.AwaitFinalStatus(flashCtx)
			if finalStatus != nil {
				saveOrinDeviceLogs(out, logPath, finalStatus)
			}
			if err != nil {
				return false, err
			}
			if !finalStatus.Success {
				return false, fmt.Errorf("device reported final flash status %q", finalStatus.Status)
			}
			return false, nil
		}},
	}

	failedID, err := runFlashSteps(fmt.Sprintf("Recovering %s with WendyOS %s", opts.DeviceName, opts.Version), steps, cancelFlash, logW)
	if err != nil {
		switch {
		case errors.Is(err, tui.ErrCancelled):
			if failedID >= orinStepCommands && massStorage != nil && massStorage.HandoffStarted {
				printOrinBadStateHint(os.Stdout, opts)
			}
			return ErrUserCancelled
		case isMacRawDiskPermissionError(err):
			printOrinFullDiskAccessHint(os.Stdout)
		case errors.Is(err, rcm.ErrUSBAccess):
			fmt.Println("\n" + usbAccessHintBox())
		case failedID >= orinStepCommands && massStorage != nil && massStorage.HandoffStarted:
			printOrinBadStateHint(os.Stdout, opts)
		case failedID == orinStepRCMBoot:
			fmt.Println(tui.WarningMessage("RCM boot failed before persistent writes; re-enter recovery mode and retry."))
		}
		return err
	}
	fmt.Println(tui.SuccessMessage(fmt.Sprintf("Recovered %s %s with WendyOS %s; the Jetson will reboot after the final LUN is released.", opts.DeviceName, strings.ToUpper(opts.Storage), opts.Version)))
	return nil
}

func resolveT234Flashpack(cacheDir string, ref flashpack.RecoveryRef, info *recoveryFlashpackInfo, detail func(string)) (*flashpack.Flashpack, bool, error) {
	extracted := flashpack.RecoveryExtractedCachePath(cacheDir, ref)
	tarball := flashpack.RecoveryTarballCachePath(cacheDir, ref)
	if _, err := os.Stat(filepath.Join(extracted, "manifest.json")); err == nil {
		fp, err := flashpack.ResolveRecovery(cacheDir, ref)
		if err == nil {
			// Reclaim a tarball an earlier interrupted run left behind: the
			// extracted tree is the cache from here on.
			_ = os.Remove(tarball)
		}
		return fp, true, err
	}
	cached := true
	if _, err := os.Stat(tarball); err != nil {
		cached = false
		if info.Checksum == "" {
			return nil, false, fmt.Errorf("manifest entry has no recovery flashpack checksum")
		}
		img := &imageInfo{DownloadURL: info.URL, ImageSize: info.SizeBytes, Version: info.Version}
		tmp, err := downloadImageInto(img, throttledDetail(detail, byteProgress))
		if err != nil {
			return nil, false, fmt.Errorf("downloading recovery flashpack: %w", err)
		}
		detail("verifying download")
		if err := verifySHA256(tmp, info.Checksum); err != nil {
			_ = os.Remove(tmp)
			return nil, false, err
		}
		if err := os.Rename(tmp, tarball); err != nil {
			_ = os.Remove(tmp)
			return nil, false, fmt.Errorf("caching recovery flashpack: %w", err)
		}
	} else {
		detail("verifying cached recovery download")
		if info.Checksum == "" {
			return nil, true, fmt.Errorf("manifest entry has no recovery flashpack checksum")
		}
		if err := verifySHA256(tarball, info.Checksum); err != nil {
			return nil, true, fmt.Errorf("cached recovery flashpack failed verification: %w", err)
		}
	}
	detail("extracting and verifying every consumed file")
	fp, err := flashpack.ResolveRecovery(cacheDir, ref)
	if err != nil {
		return nil, cached, err
	}
	// The extracted tree is the cache; drop the large .tar.zst so a version's
	// on-disk footprint isn't doubled (mirrors the pre-schema-v2 bundle flow).
	_ = os.Remove(tarball)
	return fp, cached, nil
}

// prepareT234Workspace hard-links immutable partition images into a per-run
// directory and copies only the mutable config FAT image. The extracted cache
// tree is never opened for writing.
func prepareT234Workspace(fp *flashpack.Flashpack) (workspace, layoutPath string, err error) {
	src := fp.FlashImagesDir()
	workspace, _, err = prepareMutableWorkspace(src, fp.ConfigImage())
	if err != nil {
		return "", "", err
	}
	layoutRel, err := filepath.Rel(src, fp.PartitionLayout())
	if err != nil {
		_ = os.RemoveAll(workspace)
		return "", "", err
	}
	return workspace, filepath.Join(workspace, layoutRel), nil
}

func injectRecoveryConfig(imagePath string, creds []wendyconf.WifiCredential, deviceName string, provJSON []byte, out io.Writer, detail func(string)) error {
	detail("downloading agent")
	agentBinary, agentVer, _, err := resolveAgentBinary("arm64", false)
	if err != nil {
		fmt.Fprintf(out, "warning: could not download wendy-agent (%v); using the agent baked into the image\n", err)
	} else {
		detail("agent " + agentVer)
	}
	d, err := diskfs.Open(imagePath)
	if err != nil {
		return fmt.Errorf("opening per-run config image: %w", err)
	}
	defer d.Close()
	fsys, err := d.GetFilesystem(0)
	if err != nil {
		return fmt.Errorf("reading config filesystem: %w", err)
	}
	return writeConfigFilesTo(fatWriter{fsys}, agentBinary, creds, deviceName, provJSON)
}

func t234RCMFiles(fp *flashpack.Flashpack) (order []string, memBCT, blob string, err error) {
	if len(fp.Manifest.RCMPhases) != 2 {
		return nil, "", "", fmt.Errorf("T234 flashpack must contain exactly two RCM phases")
	}
	for _, d := range fp.Manifest.RCMPhases[0] {
		order = append(order, filepath.FromSlash(d.File))
	}
	for _, d := range fp.Manifest.RCMPhases[1] {
		switch d.Type {
		case "bct_mem":
			memBCT = filepath.FromSlash(d.File)
		case "blob":
			blob = filepath.FromSlash(d.File)
		}
	}
	if len(order) == 0 || memBCT == "" || blob == "" {
		return nil, "", "", fmt.Errorf("T234 flashpack RCM phases omit bootROM files, bct_mem, or blob")
	}
	return order, memBCT, blob, nil
}

func runT234Helper(ctx context.Context, args []string, onProgress func(done, total int64)) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating wendy binary: %w", err)
	}
	// SECURITY: the target block device is identified by USB VID:PID before elevation
	// and no NOPASSWD sudoers rule ships, so this re-exec is not an unprivileged
	// escalation. A path glob cannot distinguish the gadget from the boot disk;
	// hardening this means re-resolving the node by port/serial inside the helper.
	cmd := exec.CommandContext(ctx, "sudo", append([]string{self, "__t234-write"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	cmd.WaitDelay = 5 * time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting T234 write helper: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		var done, total int64
		if n, _ := fmt.Sscanf(sc.Text(), "PROGRESS %d %d", &done, &total); n == 2 && onProgress != nil {
			onProgress(done, total)
		}
	}
	if err := cmd.Wait(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w", msg, err)
		}
		return err
	}
	return nil
}

func saveOrinDeviceLogs(out io.Writer, logPath string, st *t234.FinalStatus) {
	if st == nil || len(st.Logs) == 0 {
		return
	}
	dir := strings.TrimSuffix(logPath, ".log") + "-device-logs"
	if logPath == "" {
		var err error
		if dir, err = os.MkdirTemp("", "orin-device-logs-"); err != nil {
			return
		}
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	names := make([]string, 0, len(st.Logs))
	for name := range st.Logs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_ = os.WriteFile(filepath.Join(dir, filepath.Base(name)), st.Logs[name], 0o600)
	}
	fmt.Fprintf(out, "Device-side logs (%s): %s\n", strings.Join(names, ", "), dir)
}

func confirmOrinReady(opts t234InstallOptions) error {
	fmt.Println()
	fmt.Println(tui.Header("Recovering " + opts.DeviceName + " with WendyOS " + opts.Version))
	fmt.Println(orinRecoveryBriefingBox(opts))
	if note := orinMacFullDiskAccessNote(); note != "" {
		fmt.Println(note)
	}
	if opts.Force {
		return nil
	}
	fmt.Println()
	ok, err := tui.Confirm("Is the target connected and in recovery mode?")
	if errors.Is(err, tui.ErrCancelled) || (err == nil && !ok) {
		return ErrUserCancelled
	}
	return err
}

// orinRecoveryHints is the USB-recovery wait-UI text for a T234 Orin target,
// mirroring the cabling/button guidance in orinRecoveryBriefingBox so the
// shared wait UI never shows Thor's port/buttons for an Orin.
func orinRecoveryHints(opts t234InstallOptions) recoveryWaitHints {
	port := "the USB-C recovery/device-mode port"
	if opts.DeviceType == orinDeviceType {
		port = "the USB-C port next to the 40-pin header"
	}
	return recoveryWaitHints{
		label:       opts.DeviceName,
		cablingLine: "the USB-C cable is in " + briefPort.Render(port),
		buttonLine:  "the recovery sequence: power off, hold " + briefKey.Render("Force Recovery") + ", tap " + briefKey.Render("Reset/Power") + ", release Force Recovery",
	}
}

func orinRecoveryBriefingBox(opts t234InstallOptions) string {
	port := "the USB-C recovery/device-mode port"
	if opts.DeviceType == orinDeviceType {
		port = "the USB-C port next to the 40-pin header"
	}
	lines := []string{
		briefMarker.Render("●") + " " + briefTitle.Render("Destructive full recovery"),
		"  QSPI and " + briefKey.Render(strings.ToUpper(opts.Storage)) + " are updated together; all partitions, including /data, are erased.",
		"",
		briefMarker.Render("●") + " " + briefTitle.Render("USB-C cabling"),
		"  Connect this computer to " + briefPort.Render(port) + ".",
		"",
		briefMarker.Render("●") + " " + briefTitle.Render("Entering recovery mode"),
		"    " + briefNum.Render("1.") + " Power off the devkit and connect power.",
		"    " + briefNum.Render("2.") + " Hold " + briefKey.Render("Force Recovery") + ", tap " + briefKey.Render("Reset/Power") + ", then release Force Recovery.",
		"    " + briefNum.Render("3.") + " Keep the recovery USB-C cable attached until final SUCCESS.",
	}
	return briefBorder.Render(strings.Join(lines, "\n"))
}

// orinMacFullDiskAccessNote returns a short note (macOS only) warning that a USB
// recovery flash writes directly to the Jetson's disk, which macOS gates behind
// Full Disk Access for the terminal. Mirrors the Windows WinUSB-driver note.
// Empty string on other platforms.
func orinMacFullDiskAccessNote() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	lines := []string{
		briefMarker.Render("●") + " " + briefTitle.Render("First-time setup: Full Disk Access"),
		"  Recovery writes directly to the Jetson's disk over USB, which macOS gates",
		"  behind " + briefKey.Render("Full Disk Access") + " for your terminal. If macOS asks, choose " + briefKey.Render("Allow") + ".",
		"  To pre-grant: " + briefPort.Render("System Settings ▸ Privacy & Security ▸ Full Disk Access") + ",",
		"  enable your terminal (e.g. " + briefKey.Render("Terminal.app") + "), then quit and reopen it.",
	}
	return briefBorder.Render(strings.Join(lines, "\n"))
}

// isMacRawDiskPermissionError reports a macOS raw-disk open denied by TCC: the
// sudo'd helper is root, but macOS still returns EPERM ("operation not
// permitted") unless the terminal has Full Disk Access.
func isMacRawDiskPermissionError(err error) bool {
	return runtime.GOOS == "darwin" && err != nil && strings.Contains(err.Error(), "operation not permitted")
}

// printOrinFullDiskAccessHint explains the raw-disk permission failure and how
// to grant Full Disk Access. macOS-specific; root/sudo does not bypass TCC.
func printOrinFullDiskAccessHint(w io.Writer) {
	body := strings.Join([]string{
		thorHintTitle.Render("⚠  Permission denied opening the recovery disk"),
		"",
		"macOS blocked raw disk access — your terminal needs " + thorHintEmph.Render("Full Disk Access") + ".",
		"Running under sudo isn't enough, and macOS won't pop a prompt for it.",
		"",
		"  1. Open " + thorHintCmd.Render("System Settings ▸ Privacy & Security ▸ Full Disk Access") + ".",
		"  2. Enable your terminal (e.g. " + thorHintEmph.Render("Terminal.app") + "); use " + thorHintEmph.Render("+") + " to add it if it's missing.",
		"  3. Fully quit and reopen the terminal, then re-run the flash.",
	}, "\n")
	fmt.Fprintln(w, "\n"+thorHintBorder.Render(body))
}

func printOrinBadStateHint(w io.Writer, opts t234InstallOptions) {
	cmd := fmt.Sprintf("wendy os install --device-type %s --storage %s", opts.DeviceType, opts.Storage)
	body := strings.Join([]string{
		thorHintTitle.Render("⚠  Recovery was interrupted — the Jetson may not boot"), "",
		"QSPI or the rootfs may be partially written. Re-enter Force Recovery mode and retry:",
		thorHintCmd.Render(cmd),
	}, "\n")
	fmt.Fprintln(w, "\n"+thorHintBorder.Render(body))
}
