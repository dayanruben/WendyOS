package commands

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// downloadBmap fetches a (small) .bmap XML file to dst. The bmap is tiny, so a
// single GET with a short timeout is sufficient. Any failure is the caller's
// cue to fall back to dd.
func downloadBmap(url, dst string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("fetching bmap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bmap returned status %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating bmap file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20)); err != nil {
		return fmt.Errorf("writing bmap file: %w", err)
	}
	return nil
}

// runBmapWrite is the body of the hidden __bmap-write helper subcommand
// (Linux/macOS). It opens the raw device, reads the decompressed image from
// the supplied reader (stdin in production), and applies the bmap. It runs as
// root (re-exec'd under sudo by the parent), so it does not prompt or print
// TUI — progress is tracked by the parent counting bytes fed to stdin.
func runBmapWrite(devicePath, bmapPath string, image io.Reader) error {
	data, err := os.ReadFile(bmapPath)
	if err != nil {
		return fmt.Errorf("reading bmap: %w", err)
	}
	b, err := parseBmap(data)
	if err != nil {
		return err
	}
	dev, err := os.OpenFile(devicePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("opening device %s: %w", devicePath, err)
	}
	defer dev.Close()
	if err := applyBmap(image, dev, b, func(int64) {}); err != nil {
		return err
	}
	return dev.Sync()
}
