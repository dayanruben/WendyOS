package commands

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// The hidden "__usb-setup" subcommand is the privileged half of the USB-C
// auto-setup flow, re-executed under sudo by maybeOfferUSBSetup.
func TestNewUSBSetupHiddenCmd_Flags(t *testing.T) {
	cmd := newUSBSetupHiddenCmd()
	if cmd.Use != "__usb-setup" {
		t.Fatalf("Use = %q, want __usb-setup", cmd.Use)
	}
	if !cmd.Hidden {
		t.Error("expected __usb-setup to be hidden")
	}
	if cmd.Flags().Lookup("iface") == nil {
		t.Error("missing flag --iface")
	}
}

func TestUSBSetupModeFor(t *testing.T) {
	tests := []struct {
		goos string
		euid int
		want usbSetupMode
	}{
		{"linux", 1000, usbSetupSudo},
		{"linux", 0, usbSetupDirect},
		{"darwin", 501, usbSetupUnsupported},
		{"darwin", 0, usbSetupUnsupported},
		{"windows", -1, usbSetupUnsupported},
	}
	for _, tt := range tests {
		if got := usbSetupModeFor(tt.goos, tt.euid); got != tt.want {
			t.Errorf("usbSetupModeFor(%q, %d) = %v, want %v", tt.goos, tt.euid, got, tt.want)
		}
	}
}

func TestUSBSetupSudoArgs(t *testing.T) {
	self := "/usr/local/bin/wendy"
	if got, want := strings.Join(usbSetupSudoArgs(self, "", true), " "), self+" __usb-setup"; got != want {
		t.Errorf("interactive = %q, want %q", got, want)
	}
	if got, want := strings.Join(usbSetupSudoArgs(self, "usb0", false), " "), "-n "+self+" __usb-setup --iface usb0"; got != want {
		t.Errorf("non-interactive = %q, want %q", got, want)
	}
}

// `wendy device usb-setup` is what AGENTS.md, the docs and error hints tell
// people to run; it must be a real, visible command (it used to be "unknown").
func TestDeviceUSBSetupCmd_Resolves(t *testing.T) {
	cmd, _, err := NewRootCmd().Find([]string{"device", "usb-setup"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if cmd.CommandPath() != "wendy device usb-setup" {
		t.Fatalf("resolved to %q", cmd.CommandPath())
	}
	if cmd.Hidden {
		t.Error("usb-setup is documented and must not be hidden")
	}
	if cmd.Flags().Lookup("iface") == nil {
		t.Error("missing --iface")
	}
}

// Every backticked `wendy … usb-setup …` command in the repo docs and AGENTS.md
// must resolve to a visible command.
func TestDocumentedUSBSetupCommandsResolve(t *testing.T) {
	re := regexp.MustCompile("`(?:sudo )?(wendy [^`]*usb-setup[^`]*)`")
	root := NewRootCmd()
	found := 0
	check := func(name string, data []byte) {
		for _, m := range re.FindAllSubmatch(data, -1) {
			found++
			var words []string
			for _, f := range strings.Fields(string(m[1]))[1:] {
				if strings.HasPrefix(f, "-") {
					break
				}
				words = append(words, f)
			}
			if len(words) == 0 {
				t.Errorf("%s: `%s` names no subcommand", name, m[1])
				continue
			}
			cmd, _, err := root.Find(words)
			if err != nil || cmd.Name() != words[len(words)-1] || cmd.Hidden {
				t.Errorf("%s: `%s` does not resolve to a visible command", name, m[1])
			}
		}
	}
	agents, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	check("AGENTS.md", agents)
	docs := filepath.Join("..", "assets", "docs")
	err = filepath.WalkDir(docs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip untracked local content: installed packages, build output
			// and gitignored working notes.
			if n := d.Name(); path != docs && (n == "node_modules" || n == "superpowers" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		check(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("found no usb-setup references; the scan is broken")
	}
}

// `sudo wendy device usb-setup` runs as root. It must not run the root
// command's init (config, analytics, MCP refresh), which would write
// root-owned files into a $HOME that sudo preserved.
func TestDeviceUSBSetupCmd_SkipsRootInit(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"device", "usb-setup"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.PersistentPreRunE == nil || cmd.PersistentPostRunE == nil {
		t.Fatal("usb-setup must override the root command's persistent hooks")
	}
	if runtime.GOOS == "linux" {
		t.Skip("running usb-setup on Linux would invoke sudo")
	}
	cfgDir := t.TempDir()
	t.Setenv("WENDY_CONFIG_DIR", cfgDir)
	t.Setenv("HOME", t.TempDir())
	root.SetArgs([]string{"device", "usb-setup"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "only needed on Linux") {
		t.Fatalf("err = %v, want the Linux-only error", err)
	}
	if entries, _ := os.ReadDir(cfgDir); len(entries) != 0 {
		t.Fatalf("root init ran and wrote %v", entries)
	}
}
