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
// cue to fall back to dd. The download is atomic: data is written to a
// temporary file and renamed into place only on success, so an interrupted
// download never leaves a corrupt file for a later run to parse.
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

	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating bmap file: %w", err)
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("writing bmap file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing bmap file: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("finalizing bmap file: %w", err)
	}
	return nil
}

// countingWriter wraps an io.Writer and reports cumulative bytes written via
// progressFn. Used to drive the flash progress bar from bytes fed to the helper.
type countingWriter struct {
	w          io.Writer
	n          int64
	progressFn func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	written, err := c.w.Write(p)
	c.n += int64(written)
	if c.progressFn != nil {
		c.progressFn(c.n)
	}
	return written, err
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
