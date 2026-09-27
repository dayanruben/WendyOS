package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wendylabsinc/wendy/go/internal/cli/assets"
)

func TestCodexSkillsInstallDiscoverableDirectoriesAndReferences(t *testing.T) {
	target := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := installWendySkillDirs(target); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"wendy", "wendy-device-install", "wendy-robot-deploy", "wendy-template-app", "wendy-mcp-setup"} {
		data, err := os.ReadFile(filepath.Join(target, name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		want, _ := assets.FS.ReadFile("skills/" + name + "/SKILL.md")
		if string(data) != string(want) {
			t.Fatalf("skill %s lost content", name)
		}
	}
	path := "wendy/references/wendy.json.md"
	got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := assets.FS.ReadFile("skills/" + path)
	if string(got) != string(want) {
		t.Fatal("reference content missing")
	}
	if _, err := os.Stat(filepath.Join(target, "linear")); !os.IsNotExist(err) {
		t.Fatal("installed unrelated skill")
	}
}

func TestCodexSkillsPreserveExistingAndEditedContent(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(fmt.Sprint(installed), func(t *testing.T) {
			target := t.TempDir()
			if installed {
				if err := installWendySkillDirs(target); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(target, "wendy", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			const custom = "my customized Wendy workflow"
			if err := os.WriteFile(file, []byte(custom), 0644); err != nil {
				t.Fatal(err)
			}
			if err := installWendySkillDirs(target); err == nil || !strings.Contains(err.Error(), "preserving") {
				t.Fatalf("expected preservation error: %v", err)
			}
			got, _ := os.ReadFile(file)
			if string(got) != custom {
				t.Fatal("overwrote user skill")
			}
		})
	}
}
