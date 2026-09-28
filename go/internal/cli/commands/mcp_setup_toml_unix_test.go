//go:build unix

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// writeFileAtomic replaces the file with a new inode; it must hand the new
// file to the old one's owner, or a root run leaves the user a config file
// their AI tool can no longer read.
func TestWriteFileAtomic_KeepsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		t.Fatal(err)
	}
	type call struct{ uid, gid int }
	var calls []call
	oldChown := chownFile
	chownFile = func(f *os.File, uid, gid int) error {
		calls = append(calls, call{uid, gid})
		return errors.New("operation not permitted") // best effort: must not fail the write
	}
	t.Cleanup(func() { chownFile = oldChown })

	if err := writeFileAtomic(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if want := (call{int(st.Uid), int(st.Gid)}); len(calls) != 1 || calls[0] != want {
		t.Fatalf("chown calls = %v, want [%v]", calls, want)
	}
	if got, _ := os.ReadFile(path); string(got) != "new\n" {
		t.Fatalf("content = %q", got)
	}

	// A new file has no previous owner to keep.
	calls = nil
	if err := writeFileAtomic(filepath.Join(filepath.Dir(path), "new.toml"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("chown called for a new file: %v", calls)
	}
}
