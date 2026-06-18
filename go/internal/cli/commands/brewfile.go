package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

const defaultNativeBrewfile = "Brewfile.wendy"

func appendNativeBrewfileSyncEntry(entries []fileSyncEntry, cwd string, appCfg *appconfig.AppConfig) ([]fileSyncEntry, error) {
	entry, err := resolveNativeBrewfileSyncEntry(cwd, appCfg)
	if err != nil || entry == nil {
		return entries, err
	}

	for _, existing := range entries {
		covered, sameSource, err := syncEntryCoversBrewfile(existing, *entry)
		if err != nil {
			return entries, err
		}
		if !covered {
			continue
		}
		if sameSource {
			return entries, nil
		}
		return entries, fmt.Errorf(
			"brewfile %q conflicts with another synced file at %q; remove the duplicate files entry or point brewfile at the same source",
			appCfg.Brewfile,
			entry.remotePath,
		)
	}

	return append(entries, *entry), nil
}

func resolveNativeBrewfileSyncEntry(cwd string, appCfg *appconfig.AppConfig) (*fileSyncEntry, error) {
	if appCfg == nil {
		return nil, nil
	}

	configured := strings.TrimSpace(appCfg.Brewfile)
	if configured == "" {
		localPath := filepath.Join(cwd, defaultNativeBrewfile)
		info, err := os.Stat(localPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("checking %s: %w", defaultNativeBrewfile, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s must be a file, got directory %s", defaultNativeBrewfile, localPath)
		}
		appCfg.Brewfile = defaultNativeBrewfile
		return &fileSyncEntry{localPath: localPath, remotePath: defaultNativeBrewfile}, nil
	}

	localPath := filepath.Join(cwd, configured)
	info, err := os.Stat(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("brewfile %q declared in wendy.json does not exist", configured)
		}
		return nil, fmt.Errorf("checking brewfile %q: %w", configured, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("brewfile %q declared in wendy.json must be a file, got directory", configured)
	}

	remotePath := effectiveRemotePath(configured, "")
	appCfg.Brewfile = remotePath
	return &fileSyncEntry{localPath: localPath, remotePath: remotePath}, nil
}

func syncEntryCoversBrewfile(existing, brewfile fileSyncEntry) (bool, bool, error) {
	info, err := os.Stat(existing.localPath)
	if err != nil {
		return false, false, fmt.Errorf("checking synced file %s: %w", existing.localPath, err)
	}

	if !info.IsDir() {
		if existing.remotePath != brewfile.remotePath {
			return false, false, nil
		}
		same, err := sameLocalFile(existing.localPath, brewfile.localPath)
		if err != nil {
			return true, false, err
		}
		return true, same, nil
	}

	rel, ok := remotePathRelativeToPrefix(brewfile.remotePath, existing.remotePath)
	if !ok {
		return false, false, nil
	}

	candidate := filepath.Join(existing.localPath, rel)
	if _, err := os.Stat(candidate); err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return true, false, fmt.Errorf("checking synced brewfile source %s: %w", candidate, err)
	}

	same, err := sameLocalFile(candidate, brewfile.localPath)
	if err != nil {
		return true, false, err
	}
	return true, same, nil
}

func remotePathRelativeToPrefix(remotePath, prefix string) (string, bool) {
	remotePath = strings.TrimPrefix(remotePath, "./")
	prefix = strings.TrimPrefix(prefix, "./")
	if prefix == "" {
		return remotePath, remotePath != ""
	}
	if remotePath == prefix {
		return "", false
	}
	prefixWithSlash := prefix + "/"
	if !strings.HasPrefix(remotePath, prefixWithSlash) {
		return "", false
	}
	return strings.TrimPrefix(remotePath, prefixWithSlash), true
}

func sameLocalFile(a, b string) (bool, error) {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("checking synced brewfile source %s: %w", a, err)
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false, fmt.Errorf("checking brewfile source %s: %w", b, err)
	}
	return os.SameFile(aInfo, bInfo), nil
}
