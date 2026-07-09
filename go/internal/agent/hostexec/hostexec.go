//go:build unix

// Package hostexec runs an interactive login shell (or an explicit command) on
// the device host with a PTY. It is the host-level analogue of container exec:
// containerd owns the PTY for in-container exec, but a host shell needs its own
// os/exec + pty path.
package hostexec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/creack/pty"
)

// Spawner runs a host PTY session. It holds no state; New returns a ready value.
type Spawner struct{}

// New returns a host shell spawner.
func New() *Spawner { return &Spawner{} }

// Run starts command (or root's login shell when command is empty) attached to a
// newly allocated PTY, copies stdin into the PTY and PTY output into stdout,
// applies window resizes ([rows, cols]) until the resize channel closes, and
// returns the child's exit code. stderr is merged into the PTY master, so all
// output arrives via stdout.
//
// The caller owns stdin and resize: Run's internal goroutines that read stdin
// and drain resize unwind only when the caller closes the stdin reader and the
// resize channel. Callers must close both on every path that ends a session
// (including error and cancellation) or each session leaks two goroutines.
func (Spawner) Run(ctx context.Context, command []string, stdin io.Reader, stdout io.Writer, resize <-chan [2]uint32) (int, error) {
	argv := command
	if len(argv) == 0 {
		argv = []string{loginShell()}
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "HOME=/root", "TERM=xterm-256color")
	// Start in root's home only when running as root with /root present — that is
	// the on-device case (the agent runs as root and /root exists). Bare existence
	// is not enough: on a non-root box /root can exist as 0700 root:root, and
	// chdir(2) into it during pty.Start would fail with EACCES. Off the device
	// (dev/test/non-root CI) leave cmd.Dir unset so the child inherits the working
	// directory and the shell still starts.
	if os.Geteuid() == 0 {
		if fi, statErr := os.Stat("/root"); statErr == nil && fi.IsDir() {
			cmd.Dir = "/root"
		}
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return 0, fmt.Errorf("starting host pty for %q: %w", argv[0], err)
	}
	defer func() { _ = ptmx.Close() }()

	// Apply resizes until the channel closes.
	go func() {
		for sz := range resize {
			_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(sz[0]), Cols: uint16(sz[1])})
		}
	}()

	// stdin -> PTY master. Returns when the handler closes the stdin reader.
	go func() { _, _ = io.Copy(ptmx, stdin) }()

	// PTY master -> stdout. Returns when the child exits and the master EOFs.
	_, _ = io.Copy(stdout, ptmx)

	return exitCode(cmd.Wait()), nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// loginShell resolves root's login shell from /etc/passwd, falling back to $SHELL
// and then /bin/sh.
func loginShell() string {
	if sh := shellFromPasswd("/etc/passwd", "root"); sh != "" {
		return sh
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// shellFromPasswd returns the login shell (7th field) for username in a
// passwd-format file, or "" if the file is unreadable or the user is absent.
func shellFromPasswd(path, username string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 7 && fields[0] == username {
			return fields[6]
		}
	}
	return ""
}
