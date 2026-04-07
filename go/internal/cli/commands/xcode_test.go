package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/internal/shared/appconfig"
)

// ---------------------------------------------------------------------------
// findXcodeProj
// ---------------------------------------------------------------------------

func TestFindXcodeProj_None(t *testing.T) {
	dir := t.TempDir()
	got, err := findXcodeProj(dir)
	if err != nil {
		t.Fatalf("findXcodeProj unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("findXcodeProj = %q; want empty string", got)
	}
}

func TestFindXcodeProj_One(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "MyApp.xcodeproj"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findXcodeProj(dir)
	if err != nil {
		t.Fatalf("findXcodeProj unexpected error: %v", err)
	}
	if got != "MyApp.xcodeproj" {
		t.Errorf("findXcodeProj = %q; want %q", got, "MyApp.xcodeproj")
	}
}

func TestFindXcodeProj_Multiple_Error(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"First.xcodeproj", "Second.xcodeproj"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := findXcodeProj(dir)
	if err == nil {
		t.Fatal("findXcodeProj expected error for multiple .xcodeproj dirs, got nil")
	}
	if !strings.Contains(err.Error(), "multiple .xcodeproj") {
		t.Errorf("expected 'multiple .xcodeproj' in error, got: %v", err)
	}
}

func TestFindXcodeProj_IgnoresFiles(t *testing.T) {
	// A regular file named *.xcodeproj should not be counted.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fake.xcodeproj"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findXcodeProj(dir)
	if err != nil {
		t.Fatalf("findXcodeProj unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("findXcodeProj = %q; want empty (file should be ignored)", got)
	}
}

// ---------------------------------------------------------------------------
// parseXcodeSchemes
// ---------------------------------------------------------------------------

func TestParseXcodeSchemes_Project(t *testing.T) {
	data := []byte(`{
		"project": {
			"configurations": ["Debug","Release"],
			"name": "HelloXcode",
			"schemes": ["HelloXcode","HelloXcodeTests"],
			"targets": ["HelloXcode"]
		}
	}`)
	schemes, err := parseXcodeSchemes(data)
	if err != nil {
		t.Fatalf("parseXcodeSchemes error: %v", err)
	}
	if len(schemes) != 2 || schemes[0] != "HelloXcode" || schemes[1] != "HelloXcodeTests" {
		t.Errorf("parseXcodeSchemes = %v; want [HelloXcode HelloXcodeTests]", schemes)
	}
}

func TestParseXcodeSchemes_Workspace(t *testing.T) {
	data := []byte(`{
		"workspace": {
			"name": "MyWorkspace",
			"schemes": ["AppScheme"]
		}
	}`)
	schemes, err := parseXcodeSchemes(data)
	if err != nil {
		t.Fatalf("parseXcodeSchemes error: %v", err)
	}
	if len(schemes) != 1 || schemes[0] != "AppScheme" {
		t.Errorf("parseXcodeSchemes = %v; want [AppScheme]", schemes)
	}
}

func TestParseXcodeSchemes_EmptySchemes(t *testing.T) {
	data := []byte(`{"project": {"schemes": []}}`)
	schemes, err := parseXcodeSchemes(data)
	if err != nil {
		t.Fatalf("parseXcodeSchemes error: %v", err)
	}
	if len(schemes) != 0 {
		t.Errorf("parseXcodeSchemes = %v; want empty", schemes)
	}
}

func TestParseXcodeSchemes_NeitherKey(t *testing.T) {
	data := []byte(`{"other": {}}`)
	_, err := parseXcodeSchemes(data)
	if err == nil {
		t.Fatal("parseXcodeSchemes expected error when neither project nor workspace key present")
	}
}

func TestParseXcodeSchemes_InvalidJSON(t *testing.T) {
	_, err := parseXcodeSchemes([]byte("not json"))
	if err == nil {
		t.Fatal("parseXcodeSchemes expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// findXcodeScheme (via execCommandContext injection)
// ---------------------------------------------------------------------------

func TestFindXcodeScheme_SingleScheme(t *testing.T) {
	original := execCommandContext
	t.Cleanup(func() { execCommandContext = original })

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Return a command that writes valid JSON to stdout.
		script := `echo '{"project":{"schemes":["HelloXcode"]}}'`
		return exec.CommandContext(ctx, "sh", "-c", script)
	}

	scheme, err := findXcodeScheme(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("findXcodeScheme error: %v", err)
	}
	if scheme != "HelloXcode" {
		t.Errorf("findXcodeScheme = %q; want %q", scheme, "HelloXcode")
	}
}

func TestFindXcodeScheme_MultipleSchemes_Error(t *testing.T) {
	original := execCommandContext
	t.Cleanup(func() { execCommandContext = original })

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		script := `echo '{"project":{"schemes":["App","AppTests"]}}'`
		return exec.CommandContext(ctx, "sh", "-c", script)
	}

	_, err := findXcodeScheme(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("findXcodeScheme expected error for multiple schemes, got nil")
	}
	if !strings.Contains(err.Error(), "multiple schemes") {
		t.Errorf("expected 'multiple schemes' in error, got: %v", err)
	}
}

func TestFindXcodeScheme_NoSchemes_Error(t *testing.T) {
	original := execCommandContext
	t.Cleanup(func() { execCommandContext = original })

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		script := `echo '{"project":{"schemes":[]}}'`
		return exec.CommandContext(ctx, "sh", "-c", script)
	}

	_, err := findXcodeScheme(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("findXcodeScheme expected error when no schemes found, got nil")
	}
}

func TestFindXcodeScheme_XcodebuildMissing(t *testing.T) {
	original := execCommandContext
	t.Cleanup(func() { execCommandContext = original })

	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "nonexistent-binary-that-does-not-exist-12345")
	}

	_, err := findXcodeScheme(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("findXcodeScheme expected error when xcodebuild missing, got nil")
	}
}

// ---------------------------------------------------------------------------
// findXcodeBuildProduct
// ---------------------------------------------------------------------------

func TestFindXcodeBuildProduct_Binary(t *testing.T) {
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a fake binary.
	if err := os.WriteFile(filepath.Join(releaseDir, "MyCLI"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, isApp, err := findXcodeBuildProduct(derived, "MyCLI")
	if err != nil {
		t.Fatalf("findXcodeBuildProduct error: %v", err)
	}
	if isApp {
		t.Error("isApp should be false for plain binary")
	}
	if filepath.Base(path) != "MyCLI" {
		t.Errorf("product path base = %q; want %q", filepath.Base(path), "MyCLI")
	}
}

func TestFindXcodeBuildProduct_AppBundle(t *testing.T) {
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	appDir := filepath.Join(releaseDir, "MyApp.app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create a fake binary inside the bundle.
	macosDir := filepath.Join(appDir, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macosDir, "MyApp"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, isApp, err := findXcodeBuildProduct(derived, "MyApp")
	if err != nil {
		t.Fatalf("findXcodeBuildProduct error: %v", err)
	}
	if !isApp {
		t.Error("isApp should be true for .app bundle")
	}
	if filepath.Base(path) != "MyApp.app" {
		t.Errorf("product path base = %q; want %q", filepath.Base(path), "MyApp.app")
	}
}

func TestFindXcodeBuildProduct_AppBundleTakesPrecedenceOverBinary(t *testing.T) {
	// When both a .app dir and a plain binary exist, .app wins.
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	if err := os.MkdirAll(filepath.Join(releaseDir, "MyApp.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "MyApp"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, isApp, err := findXcodeBuildProduct(derived, "MyApp")
	if err != nil {
		t.Fatalf("findXcodeBuildProduct error: %v", err)
	}
	if !isApp {
		t.Error("isApp should be true when .app dir present alongside binary")
	}
}

func TestFindXcodeBuildProduct_NotFound(t *testing.T) {
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := findXcodeBuildProduct(derived, "Ghost")
	if err == nil {
		t.Fatal("findXcodeBuildProduct expected error when product not found, got nil")
	}
}

// ---------------------------------------------------------------------------
// xcodeEntrypoint
// ---------------------------------------------------------------------------

func TestXcodeEntrypoint(t *testing.T) {
	tests := []struct {
		productPath string
		isApp       bool
		want        string
	}{
		{"/path/to/Build/Products/Release/MyCLI", false, "MyCLI"},
		{"/path/to/Build/Products/Release/MyApp.app", true, "MyApp.app/Contents/MacOS/MyApp"},
		{"/path/to/Build/Products/Release/HelloXcode.app", true, "HelloXcode.app/Contents/MacOS/HelloXcode"},
		{"/path/to/Build/Products/Release/Tool", false, "Tool"},
	}
	for _, tt := range tests {
		got := xcodeEntrypoint(tt.productPath, tt.isApp)
		if got != tt.want {
			t.Errorf("xcodeEntrypoint(%q, %v) = %q; want %q", tt.productPath, tt.isApp, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// assembleXcodeSyncEntries
// ---------------------------------------------------------------------------

func TestAssembleXcodeSyncEntries_Binary(t *testing.T) {
	// Build a fake Release directory with a binary and a sibling .bundle.
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(releaseDir, "MyCLI")
	if err := os.WriteFile(binPath, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	bundleDir := filepath.Join(releaseDir, "Resources.bundle")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "info.plist"), []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	cfg := &appconfig.AppConfig{AppID: "mycli"}

	entries, err := assembleXcodeSyncEntries(binPath, false, cwd, cfg)
	if err != nil {
		t.Fatalf("assembleXcodeSyncEntries error: %v", err)
	}

	// Expect binary + bundle entries.
	remotes := make(map[string]bool)
	for _, e := range entries {
		remotes[e.remotePath] = true
	}
	if !remotes["MyCLI"] {
		t.Error("expected binary entry with remotePath MyCLI")
	}
	if !remotes["Resources.bundle"] {
		t.Error("expected bundle entry with remotePath Resources.bundle")
	}
}

func TestAssembleXcodeSyncEntries_AppBundle(t *testing.T) {
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	appPath := filepath.Join(releaseDir, "MyApp.app")
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	cfg := &appconfig.AppConfig{AppID: "myapp"}

	entries, err := assembleXcodeSyncEntries(appPath, true, cwd, cfg)
	if err != nil {
		t.Fatalf("assembleXcodeSyncEntries error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for .app bundle, got %d", len(entries))
	}
	if entries[0].remotePath != "MyApp.app" {
		t.Errorf("remotePath = %q; want %q", entries[0].remotePath, "MyApp.app")
	}
	if entries[0].localPath != appPath {
		t.Errorf("localPath = %q; want %q", entries[0].localPath, appPath)
	}
}

func TestAssembleXcodeSyncEntries_SandboxAndFiles(t *testing.T) {
	derived := t.TempDir()
	releaseDir := filepath.Join(derived, "Build", "Products", "Release")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(releaseDir, "Tool")
	if err := os.WriteFile(binPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd := t.TempDir()
	// Create sandbox.sb in the project directory.
	if err := os.WriteFile(filepath.Join(cwd, "sandbox.sb"), []byte("(version 1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a user file.
	if err := os.WriteFile(filepath.Join(cwd, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &appconfig.AppConfig{
		AppID: "tool",
		Files: []appconfig.FileSyncEntry{{Path: "config.json"}},
	}

	entries, err := assembleXcodeSyncEntries(binPath, false, cwd, cfg)
	if err != nil {
		t.Fatalf("assembleXcodeSyncEntries error: %v", err)
	}

	remotes := make(map[string]bool)
	for _, e := range entries {
		remotes[e.remotePath] = true
	}
	if !remotes["sandbox.sb"] {
		t.Error("expected sandbox.sb entry")
	}
	if !remotes["config.json"] {
		t.Error("expected config.json entry from wendy.json Files")
	}
}
