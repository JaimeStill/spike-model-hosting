//go:build unix

package gate

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// groupWait bounds how long a cancelled suite's command waits for its output to close once its
// process group is killed.
const groupWait = 10 * time.Second

// groupCommand is exec.CommandContext for a suite's subprocess (uv and the Python it runs,
// GuideLLM), started in a process group of its own, whose whole group is killed when ctx is done.
// uv runs the suite as its child, and Inspect and MTEB start workers of their own, so killing uv
// alone, as exec.CommandContext does, leaves them running: still loading the router, and writing
// into a run directory a cancelled run has removed. An interrupt cancels evaluate's context, so
// it kills the group too.
func groupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = groupWait
	return cmd
}
