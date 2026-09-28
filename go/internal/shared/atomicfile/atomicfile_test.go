package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesTheFileWithTheGivenMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device-key.pem")
	if err := Write(path, []byte("key material\n"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "key material\n" {
		t.Errorf("contents = %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("mode = %v, want 0600", got)
		}
	}
}

func TestWriteReplacesAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "device.pem")
	if err := Write(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Write(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("Write (replace): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("contents = %q, want the replacement", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the target: %v", len(entries), entries)
	}
}

func TestWriteFailsOnAMissingDirectory(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "nope", "device.pem"), []byte("x"), 0o600); err == nil {
		t.Error("Write into a missing directory returned no error")
	}
}

// stubHost fakes the platform and the rename for the Windows-only branches,
// which the Linux and macOS runners cannot otherwise reach.
func stubHost(t *testing.T, goos string, rename func(string, string) error) {
	t.Helper()
	prevOS, prevRename := hostOS, renameFn
	hostOS, renameFn = goos, rename
	t.Cleanup(func() { hostOS, renameFn = prevOS, prevRename })
}

func TestWriteRetriesARenameWindowsRefused(t *testing.T) {
	calls := 0
	stubHost(t, "windows", func(from, to string) error {
		calls++
		if calls < 3 {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errors.New("Access is denied.")}
		}
		return os.Rename(from, to)
	})
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("Write gave up on a transient Windows rename refusal: %v", err)
	}
	if calls != 3 {
		t.Errorf("rename called %d times, want 3 (two refusals, then success)", calls)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Errorf("contents = %q (err %v), want %q", data, err, "new")
	}
}

func TestWriteDoesNotRetryARenameElsewhere(t *testing.T) {
	calls := 0
	stubHost(t, "linux", func(string, string) error {
		calls++
		return errors.New("rename refused")
	})
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "config.json"), []byte("x"), 0o600); err == nil {
		t.Fatal("Write succeeded although the rename failed")
	}
	if calls != 1 {
		t.Errorf("rename called %d times off Windows, want exactly 1", calls)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed write left %v behind", entries)
	}
}
