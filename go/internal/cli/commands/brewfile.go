package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
)

const defaultNativeBrewfile = "Brewfile"

func appendNativeBrewfileSyncEntry(entries []fileSyncEntry, cwd string, appCfg *appconfig.AppConfig) ([]fileSyncEntry, error) {
	entry, err := resolveNativeBrewfileSyncEntry(cwd, appCfg)
	if err != nil || entry == nil {
		return entries, err
	}

	for _, existing := range entries {
		if existing.remotePath == entry.remotePath {
			return entries, nil
		}
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
			return nil, fmt.Errorf("checking Brewfile: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("Brewfile must be a file, got directory %s", localPath)
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
