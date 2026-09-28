//go:build windows

package mcp

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func configureRunProcess(*exec.Cmd) {}

// signalRunProcess ends the run's process tree. Windows cannot deliver an
// interrupt to a console-less child, so every stage terminates.
func signalRunProcess(cmd *exec.Cmd, _ int) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
