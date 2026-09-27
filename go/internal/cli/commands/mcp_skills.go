package commands

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wendylabsinc/wendy/go/internal/cli/assets"
	"github.com/wendylabsinc/wendy/go/internal/shared/version"
)

// installSkillsForAllTools extracts embedded skill files into each detected AI tool.
// Errors per-tool are collected and returned so the caller can report them.
func installSkillsForAllTools() []mcpSetupResult {
	var results []mcpSetupResult

	if r := installClaudeCodeSkills(); r != nil {
		results = append(results, *r)
	}
	if r := installCodexSkills(); r != nil {
		results = append(results, *r)
	}
	if r := installOpencodeSkills(); r != nil {
		results = append(results, *r)
	}

	return results
}

// skillNames lists every first-level directory under assets/skills that holds a SKILL.md.
func wendySkillNames() []string {
	entries, err := assets.FS.ReadDir("skills")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(assets.FS, "skills/"+e.Name()+"/SKILL.md"); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

// ---- Claude Code ----------------------------------------------------------------

func installClaudeCodeSkills() *mcpSetupResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if _, err := os.Stat(pluginsDir); err != nil {
		// Claude Code not present.
		return nil
	}

	const marketplace = "wendy-skills"
	ver := sanitizeVersion(version.Version)

	skillNames := wendySkillNames()
	if len(skillNames) == 0 {
		return &mcpSetupResult{tool: "Claude Code skills", err: fmt.Errorf("no embedded skills found")}
	}

	for _, name := range skillNames {
		dst := filepath.Join(pluginsDir, "cache", marketplace, name, ver)
		if err := extractSkillDir(name, dst); err != nil {
			return &mcpSetupResult{tool: "Claude Code skills", err: fmt.Errorf("extracting %s: %w", name, err)}
		}
		if err := updateInstalledPlugins(pluginsDir, name, marketplace, ver, dst); err != nil {
			return &mcpSetupResult{tool: "Claude Code skills", err: err}
		}
	}

	return &mcpSetupResult{tool: "Claude Code skills", path: filepath.Join(pluginsDir, "cache", marketplace)}
}

// extractSkillDir copies assets/skills/<name>/** into dstDir/skills/<name>/.
func extractSkillDir(skillName, dstDir string) error {
	return extractSkillFiles(skillName, filepath.Join(dstDir, "skills", skillName))
}

func extractSkillFiles(skillName, target string) error {
	return fs.WalkDir(assets.FS, "skills/"+skillName, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "skills/"+skillName+"/")
		dst := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := assets.FS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

type installedPluginsFile struct {
	Version int                      `json:"version"`
	Plugins map[string][]pluginEntry `json:"plugins"`
}

type pluginEntry struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt,omitempty"`
	LastUpdated string `json:"lastUpdated"`
}

func updateInstalledPlugins(pluginsDir, name, marketplace, ver, installPath string) error {
	jsonPath := filepath.Join(pluginsDir, "installed_plugins.json")
	var ipf installedPluginsFile
	data, err := os.ReadFile(jsonPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading installed_plugins.json: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &ipf); err != nil {
			return fmt.Errorf("parsing installed_plugins.json: %w", err)
		}
	}
	if ipf.Version == 0 {
		ipf.Version = 2
	}
	if ipf.Plugins == nil {
		ipf.Plugins = map[string][]pluginEntry{}
	}

	key := name + "@" + marketplace
	now := time.Now().UTC().Format(time.RFC3339)
	entry := pluginEntry{
		Scope:       "user",
		InstallPath: installPath,
		Version:     ver,
		LastUpdated: now,
	}

	existing := ipf.Plugins[key]
	if len(existing) == 0 {
		entry.InstalledAt = now
		ipf.Plugins[key] = []pluginEntry{entry}
	} else {
		entry.InstalledAt = existing[0].InstalledAt
		ipf.Plugins[key] = []pluginEntry{entry}
	}

	out, err := json.MarshalIndent(ipf, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(jsonPath, out, 0o644)
}

// sanitizeVersion replaces characters not safe in directory names.
func sanitizeVersion(v string) string {
	r := strings.NewReplacer("/", "-", ":", "-", " ", "-")
	return r.Replace(v)
}

// ---- Codex ----------------------------------------------------------------------

func installCodexSkills() *mcpSetupResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	codexDir := filepath.Join(home, ".codex")
	if _, err := os.Stat(codexDir); err != nil {
		if _, err2 := exec.LookPath("codex"); err2 != nil {
			return nil
		}
		if err := os.MkdirAll(codexDir, 0o755); err != nil {
			return &mcpSetupResult{tool: "Codex skills", err: err}
		}
	}

	// Codex discovers individual SKILL.md directories, including their relative
	// references. A loose concatenated Markdown file is not a discoverable skill.
	target := filepath.Join(home, ".agents", "skills")
	if err := installWendySkillDirs(target); err != nil {
		return &mcpSetupResult{tool: "Codex skills", err: err}
	}
	return &mcpSetupResult{tool: "Codex skills", path: target}
}

func installWendySkillDirs(target string) error {
	for _, name := range wendySkillNames() {
		if name != "wendy" && !strings.HasPrefix(name, "wendy-") {
			continue
		}
		if err := installManagedSkill(name, filepath.Join(target, name)); err != nil {
			return fmt.Errorf("installing %s: %w", name, err)
		}
	}
	return nil
}

// The shared user skill directory can contain hand-written or plugin-sourced
// Wendy skills. Only replace files from our previous install that remain
// unmodified, or files that already equal the current embedded version.
func installManagedSkill(name, target string) error {
	marker := filepath.Join(target, ".wendy-managed.json")
	previous := map[string]string{}
	if data, err := os.ReadFile(marker); err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			return fmt.Errorf("reading skill ownership: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	next := map[string]string{}
	err := fs.WalkDir(assets.FS, "skills/"+name, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "skills/"+name+"/")
		data, err := assets.FS.ReadFile(p)
		if err != nil {
			return err
		}
		next[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		path := filepath.Join(target, rel)
		existing, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(existing))
		if hash != next[rel] && hash != previous[rel] {
			return fmt.Errorf("preserving existing or edited skill file %s; move that skill aside before reinstalling Wendy's version", path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := extractSkillFiles(name, target); err != nil {
		return err
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(marker, data, 0o644)
}

// ---- Opencode -------------------------------------------------------------------

func installOpencodeSkills() *mcpSetupResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Detect opencode via binary or config directory.
	configDir := filepath.Join(home, ".config", "opencode")
	if _, err := os.Stat(configDir); err != nil {
		if _, err2 := exec.LookPath("opencode"); err2 != nil {
			return nil
		}
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			return &mcpSetupResult{tool: "Opencode skills", err: err}
		}
	}

	target := filepath.Join(configDir, "wendy-skills.md")
	if err := writeSkillsMarkdown(target); err != nil {
		return &mcpSetupResult{tool: "Opencode skills", err: err}
	}
	return &mcpSetupResult{tool: "Opencode skills", path: target}
}

func writeSkillsMarkdown(path string) error {
	var sb strings.Builder
	sb.WriteString("# Wendy Skills\n\n")
	sb.WriteString("Auto-generated by `wendy mcp setup`. Do not edit — re-run the command to update.\n\n")

	for _, name := range []string{"wendy", "wendy-lite", "wendy-contributing", "wendy-swift"} {
		data, err := assets.FS.ReadFile("skills/" + name + "/SKILL.md")
		if err != nil {
			continue
		}
		sb.WriteString("---\n\n")
		sb.Write(data)
		sb.WriteString("\n\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
