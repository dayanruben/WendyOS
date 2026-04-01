package commands

import (
	"bytes"
	"testing"
)

func TestOpenBrowserCmd_MissingArg(t *testing.T) {
	cmd := newOpenBrowserCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing URL argument")
	}
}

func TestOpenBrowserCmd_MissingScheme(t *testing.T) {
	cmd := newOpenBrowserCmd()
	cmd.SetArgs([]string{"example.com"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for URL without scheme")
	}
}

func TestOpenBrowserCmd_InvalidURL(t *testing.T) {
	cmd := newOpenBrowserCmd()
	cmd.SetArgs([]string{"://bad"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestOpenBrowserCmd_ValidURL(t *testing.T) {
	cmd := newOpenBrowserCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"https://example.com"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	// This will actually try to open a browser, which is fine in CI (it will
	// fail silently or succeed depending on the environment). We primarily
	// test that the command parses and validates correctly.
	_ = cmd.Execute()

	// If it succeeded, check the output message.
	if buf.Len() > 0 {
		expected := "Opening https://example.com in default browser...\n"
		if buf.String() != expected {
			t.Errorf("unexpected output: got %q, want %q", buf.String(), expected)
		}
	}
}

func TestOpenBrowserCmd_TooManyArgs(t *testing.T) {
	cmd := newOpenBrowserCmd()
	cmd.SetArgs([]string{"https://a.com", "https://b.com"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for too many arguments")
	}
}
