//go:build linux

package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// TestWatchFlash_SuspendedDevice reproduces a flash wedged in kernel I/O: a
// suspended device-mapper target queues every write, so the writer sleeps
// uninterruptibly, as it does behind a stuck macOS FAT driver. Needs root:
//
//	go test -c ./internal/cli/commands -o /tmp/commands.test
//	sudo WENDY_E2E_DMSETUP=1 /tmp/commands.test -test.run SuspendedDevice -test.v
func TestWatchFlash_SuspendedDevice(t *testing.T) {
	if os.Getenv("WENDY_E2E_DMSETUP") == "" || os.Geteuid() != 0 {
		t.Skip("set WENDY_E2E_DMSETUP=1 and run as root")
	}
	old := stallCheckInterval
	stallCheckInterval = 100 * time.Millisecond
	t.Cleanup(func() { stallCheckInterval = old })

	dev, resume := suspendedDevice(t)
	f, err := os.OpenFile(dev, os.O_WRONLY|syscall.O_DIRECT|os.O_SYNC, 0)
	if err != nil {
		t.Fatalf("open %s: %v", dev, err)
	}
	writerDone := make(chan struct{})
	// Close waits for in-flight I/O, so resume and let the writer drain first.
	t.Cleanup(func() {
		resume()
		select {
		case <-writerDone:
		case <-time.After(10 * time.Second):
			t.Error("writer still blocked after resume")
		}
		f.Close()
	})

	// O_DIRECT bypasses the page cache, so the first write already blocks.
	buf := alignedBlock(4096)
	write := func(progress func(int64)) error {
		defer close(writerDone)
		for off := int64(0); off < 1<<20; off += int64(len(buf)) {
			if _, err := f.WriteAt(buf, off); err != nil {
				return err
			}
			progress(off + int64(len(buf)))
		}
		return nil
	}

	start := time.Now()
	err = watchFlash(write, 1<<20, fixedLimit(2*time.Second), nil)
	if !errors.Is(err, errFlashStalled) {
		t.Fatalf("watchFlash error = %v, want errFlashStalled", err)
	}
	t.Logf("stall reported after %s", time.Since(start).Round(100*time.Millisecond))

	// The writer must still be stuck: watchFlash returned without it.
	select {
	case <-writerDone:
		t.Fatal("writer finished; the device was not actually blocking")
	default:
	}
}

// suspendedDevice returns a suspended dm-linear device backed by a loop file,
// and a func that resumes it.
func suspendedDevice(t *testing.T) (string, func()) {
	t.Helper()
	img := t.TempDir() + "/sd.img"
	if err := os.WriteFile(img, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(img, 64<<20); err != nil {
		t.Fatal(err)
	}
	loop := strings.TrimSpace(mustRun(t, "losetup", "-f", "--show", img))
	t.Cleanup(func() { exec.Command("losetup", "-d", loop).Run() })

	name := fmt.Sprintf("wendy-stall-%d", os.Getpid())
	sectors := strings.TrimSpace(mustRun(t, "blockdev", "--getsz", loop))
	table := fmt.Sprintf("0 %s linear %s 0", sectors, loop)
	create := exec.Command("dmsetup", "create", name)
	create.Stdin = strings.NewReader(table)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("dmsetup create: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("dmsetup", "remove", "--retry", name).Run() })

	mustRun(t, "dmsetup", "suspend", name)
	resume := func() { exec.Command("dmsetup", "resume", name).Run() }
	t.Cleanup(resume)
	return "/dev/mapper/" + name, resume
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// alignedBlock returns a size-byte buffer aligned for O_DIRECT.
func alignedBlock(size int) []byte {
	raw := make([]byte, size*2)
	off := size - int(uintptr(unsafe.Pointer(&raw[0]))%uintptr(size))
	if off == size {
		off = 0
	}
	return raw[off : off+size]
}
