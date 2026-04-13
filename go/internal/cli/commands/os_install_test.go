//go:build darwin || linux || windows

package commands

import (
	"testing"

	"github.com/wendylabsinc/wendy/internal/shared/version"
)

func TestNewOSInstallCmd_Flags(t *testing.T) {
	cmd := newOSInstallCmd()
	if cmd.Use != "install [image] [drive]" {
		t.Errorf("Use = %q; want %q", cmd.Use, "install [image] [drive]")
	}

	expectedFlags := []string{"nightly", "force", "device-type", "version", "drive"}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag %q", name)
		}
	}
}

func TestNewOSInstallCmd_NightlyVersionMutualExclusion(t *testing.T) {
	cmd := newOSInstallCmd()
	cmd.SetArgs([]string{"--nightly", "--version", "0.10.0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --nightly and --version are both set")
	}
	if got := err.Error(); got != "--nightly and --version are mutually exclusive" {
		t.Errorf("unexpected error: %q", got)
	}
}

func TestNewOSInstallCmd_PositionalArgsIncompatibleWithFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"positional with --device-type", []string{"image.img", "/dev/disk4", "--device-type", "raspberry-pi-5", "--force"}},
		{"positional with --version", []string{"image.img", "/dev/disk4", "--version", "0.10.0", "--force"}},
		{"positional with --drive", []string{"image.img", "/dev/disk4", "--drive", "/dev/disk5", "--force"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newOSInstallCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error when positional args are combined with manifest flags")
			}
			expected := "positional [image] [drive] arguments cannot be combined with --device-type, --version, or --drive"
			if got := err.Error(); got != expected {
				t.Errorf("unexpected error: %q; want %q", got, expected)
			}
		})
	}
}

func TestPickInstallVersion_SemverOrdering(t *testing.T) {
	// Verify that version keys are sorted semantically, not lexicographically.
	// "0.10.0" should come after "0.9.0" semantically but before it lexicographically.
	versions := []string{"0.2.0", "0.10.0", "0.9.0", "0.1.0", "0.10.1"}

	// Use the same sorting logic as pickInstallVersion.
	sorted := make([]string, len(versions))
	copy(sorted, versions)
	// Import sort inline since the test file uses testing directly.
	sortFunc := func(i, j int) bool {
		return version.CompareVersions(sorted[i], sorted[j]) > 0
	}

	// Manual sort to avoid importing "sort" — just use a simple bubble sort for testing.
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if !sortFunc(i, j) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	expected := []string{"0.10.1", "0.10.0", "0.9.0", "0.2.0", "0.1.0"}
	for i, v := range sorted {
		if v != expected[i] {
			t.Errorf("sorted[%d] = %q; want %q (full: %v)", i, v, expected[i], sorted)
			break
		}
	}
}

func TestOsCachedImagePath_Sanitization(t *testing.T) {
	// Valid inputs should produce a valid path.
	path, err := osCachedImagePath("raspberry-pi-5", "0.10.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}

	// Path traversal in version should be rejected.
	_, err = osCachedImagePath("raspberry-pi-5", "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal in version")
	}

	// Path traversal in device key should be rejected.
	_, err = osCachedImagePath("../evil", "0.10.4")
	if err == nil {
		t.Fatal("expected error for path traversal in device key")
	}
}
