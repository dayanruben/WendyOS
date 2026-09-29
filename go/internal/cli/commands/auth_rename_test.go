//go:build darwin || linux || windows

package commands

import "testing"

// rename <old> <new> renames a context by name.
func TestAuthRenameTwoArgs(t *testing.T) {
	load := seedConfig(t, sharedEndpointConfig())

	cmd := newAuthRenameCmd()
	cmd.SetArgs([]string{"default", "acme"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg := load()
	if _, ok := cfg.ContextByName("acme"); !ok {
		t.Fatal("renamed context 'acme' not found")
	}
	if _, ok := cfg.ContextByName("default"); ok {
		t.Fatal("old name 'default' should be gone")
	}
}

// rename <new> (one arg) renames the current context and retargets it.
func TestAuthRenameCurrentRetargets(t *testing.T) {
	seeded := sharedEndpointConfig()
	seeded.CurrentContext = "org-75"
	load := seedConfig(t, seeded)

	cmd := newAuthRenameCmd()
	cmd.SetArgs([]string{"prod"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg := load()
	if cfg.CurrentContext != "prod" {
		t.Fatalf("current context not retargeted, got %q", cfg.CurrentContext)
	}
	if _, ok := cfg.ContextByName("prod"); !ok {
		t.Fatal("renamed context 'prod' not found")
	}
}

// rename onto an existing name is refused.
func TestAuthRenameCollision(t *testing.T) {
	seedConfig(t, sharedEndpointConfig())

	cmd := newAuthRenameCmd()
	cmd.SetArgs([]string{"default", "org-75"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("renaming onto an existing context name should error")
	}
}
